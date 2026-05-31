#!/usr/bin/env bash
set -euo pipefail

readonly DEFAULT_BASE_URL="https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw"
readonly DEFAULT_PROFILE="wecom-ai-websocket"
readonly DEFAULT_BIN_SUBDIR=".local/bin"
readonly DEFAULT_CONFIG_SUBDIR=".trpc-agent-go/openclaw"
readonly DEFAULT_DEPS_PROFILE="common-file-tools"
readonly PROFILE_DIR_NAME="profiles"
readonly skillsDirName="skills"
readonly bundledSkillsDirName="bundled"
readonly localSkillsDirName="local"

readonly releasesDirName="releases"
readonly latestDirName="latest"
readonly previewDirName="preview"
readonly defaultReleaseChannel="$latestDirName"
readonly versionFileName="VERSION"
readonly checksumsFileName="checksums.txt"

readonly packageRootName="trpc-claw"
readonly binaryName="trpc-claw"
readonly deprecatedLauncherName="trpc-claw-launcher"
readonly sqliteEnabledValue="enabled"
readonly wecomAIProfileName="wecom-ai"
readonly wecomAIWebSocketProfileName="wecom-ai-websocket"
readonly wecomNotificationProfileName="wecom-notification"
readonly weixinProfileName="weixin"
readonly legacyBinaryName="openclaw"

TEMP_ROOT=""
DOWNLOAD_CLIENT=""

usage() {
  cat <<'EOF'
Install the internal trpc-claw distribution from Tencent Mirrors.

Usage:
  install.sh [options]

Options:
  --version <version>       Install a specific version.
  --channel <latest|preview>
                            Resolve this channel when --version is absent.
  --profile <name>          Config profile:
                            mock | wecom-ai | wecom-ai-websocket |
                            wecom-notification | weixin
  --base-url <url>          Artifact base URL.
  --bin-dir <dir>           Install binary to this directory.
  --config-dir <dir>        Install configs to this directory.
  -f, --force-config        Overwrite openclaw.yaml and trpc_go.yaml.
  --bootstrap-deps          Install bundled skill deps after install.
  --deps-profile <name>     Extra dependency profile for bootstrap-deps.
  -h, --help                Show help.
EOF
}

log() {
  printf '%s\n' "$*"
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

warn() {
  printf 'warning: %s\n' "$*" >&2
}

has_cmd() {
  command -v "$1" >/dev/null 2>&1
}

require_cmd() {
  if ! has_cmd "$1"; then
    die "missing required command: $1"
  fi
}

cleanup() {
  if [ -n "$TEMP_ROOT" ] && [ -d "$TEMP_ROOT" ]; then
    rm -rf "$TEMP_ROOT"
  fi
}

trim_trailing_slash() {
  local value="$1"

  printf '%s' "${value%/}"
}

detect_os() {
  case "$(uname -s)" in
    Linux)
      printf 'linux'
      ;;
    Darwin)
      printf 'darwin'
      ;;
    *)
      die "unsupported OS: $(uname -s)"
      ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64 | amd64)
      printf 'amd64'
      ;;
    arm64 | aarch64)
      printf 'arm64'
      ;;
    *)
      die "unsupported architecture: $(uname -m)"
      ;;
  esac
}

profile_file_name() {
  case "$1" in
    mock)
      printf 'openclaw.mock.yaml'
      ;;
    wecom-ai)
      printf 'openclaw.wecom.ai.yaml'
      ;;
    wecom-ai-websocket)
      printf 'openclaw.wecom.ai.websocket.yaml'
      ;;
    wecom-notification)
      printf 'openclaw.wecom.notification.yaml'
      ;;
    weixin)
      printf 'openclaw.weixin.yaml'
      ;;
    *)
      die "unsupported profile: $1"
      ;;
  esac
}

select_download_client() {
  if has_cmd curl; then
    DOWNLOAD_CLIENT="curl"
    return
  fi
  if has_cmd wget; then
    DOWNLOAD_CLIENT="wget"
    return
  fi
  die "missing required command: curl or wget"
}

download_text() {
  local url="$1"

  case "$DOWNLOAD_CLIENT" in
    curl)
      curl -fsSL --retry 3 "$url"
      ;;
    wget)
      wget -qO- --tries=3 "$url"
      ;;
    *)
      die "download client is not initialized"
      ;;
  esac
}

download_file() {
  local url="$1"
  local output="$2"

  case "$DOWNLOAD_CLIENT" in
    curl)
      curl -fsSL --retry 3 "$url" -o "$output"
      ;;
    wget)
      wget -qO "$output" --tries=3 "$url"
      ;;
    *)
      die "download client is not initialized"
      ;;
  esac
}

resolve_version() {
  local requested="$1"
  local base_url="$2"
  local channel="$3"

  if [ -n "$requested" ]; then
    printf '%s' "$requested"
    return
  fi

  download_text \
    "$(trim_trailing_slash "$base_url")/${channel}/${versionFileName}" \
    | tr -d '[:space:]'
}

normalize_channel() {
  case "$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')" in
    "" | "$latestDirName")
      printf '%s' "$latestDirName"
      ;;
    "$previewDirName")
      printf '%s' "$previewDirName"
      ;;
    *)
      die "unsupported release channel: $1"
      ;;
  esac
}

find_checksum() {
  local checksums_file="$1"
  local archive_name="$2"

  awk \
    -v name="$archive_name" \
    '{
      candidate = $2
      sub(/^\.\//, "", candidate)
      sub(/^\*/, "", candidate)
      if (candidate == name) {
        print $1
        exit
      }
    }' \
    "$checksums_file"
}

sha256_file() {
  local path="$1"

  if has_cmd sha256sum; then
    sha256sum "$path" | awk '{ print $1 }'
    return
  fi
  if has_cmd shasum; then
    shasum -a 256 "$path" | awk '{ print $1 }'
    return
  fi
  if has_cmd openssl; then
    openssl dgst -sha256 "$path" | awk '{ print $NF }'
    return
  fi

  printf ''
}

verify_checksum() {
  local checksums_file="$1"
  local archive_path="$2"
  local archive_name expected actual

  archive_name="$(basename "$archive_path")"
  expected="$(find_checksum "$checksums_file" "$archive_name")"
  [ -n "$expected" ] || die "missing checksum for ${archive_name}"

  actual="$(sha256_file "$archive_path")"
  if [ -z "$actual" ]; then
    warn "sha256sum/shasum/openssl not found; skip checksum validation"
    return
  fi

  [ "$expected" = "$actual" ] || die "checksum mismatch for ${archive_name}"
}

install_file() {
  local mode="$1"
  local source_path="$2"
  local target_path="$3"

  if has_cmd install; then
    install -m "$mode" "$source_path" "$target_path"
    return
  fi

  cp "$source_path" "$target_path"
  chmod "$mode" "$target_path"
}

sync_dir_contents() {
  local source_dir="$1"
  local target_dir="$2"

  mkdir -p "$target_dir"
  (
    cd "$source_dir"
    tar -cf - .
  ) | (
    cd "$target_dir"
    tar -xf -
  )
}

cleanup_legacy_binary() {
  local binary_path="$1"
  local legacy_path="$2"

  if [ "$binary_path" = "$legacy_path" ]; then
    return
  fi

  if [ ! -e "$legacy_path" ] && [ ! -L "$legacy_path" ]; then
    return
  fi

  if [ -L "$legacy_path" ] && has_cmd readlink; then
    local target=""
    target="$(readlink "$legacy_path" 2>/dev/null || true)"
    case "$target" in
      "$(basename "$binary_path")" | \
        "./$(basename "$binary_path")")
        rm -f "$legacy_path"
        log "Removed legacy binary: ${legacy_path}"
        return
        ;;
    esac
  fi

  if [ -f "$legacy_path" ] && has_cmd cmp && \
    cmp -s "$binary_path" "$legacy_path"; then
    rm -f "$legacy_path"
    log "Removed legacy binary: ${legacy_path}"
    return
  fi

  warn "legacy binary exists at ${legacy_path}; leaving it untouched"
}

cleanup_deprecated_launcher() {
  local launcher_path="$1"

  if [ ! -e "$launcher_path" ] && [ ! -L "$launcher_path" ]; then
    return
  fi

  rm -f "$launcher_path"
  log "Removed deprecated launcher: ${launcher_path}"
}

print_path_hint() {
  local bin_dir="$1"

  case ":${PATH}:" in
    *":${bin_dir}:"*)
      ;;
    *)
      log ""
      log "Add this directory to PATH before using ${binaryName}:"
      log "  export PATH=\"${bin_dir}:\$PATH\""
      ;;
  esac
}

print_wecom_env_hint() {
  local profile="$1"

  log ""
  log "Recommended: put model and WeCom settings in ~/.bashrc:"
  log "  export OPENAI_MODEL='gpt-5.2'"
  log "  export OPENAI_API_KEY='replace-with-your-api-key'"
  log \
    "  export OPENAI_BASE_URL='https://your-openai-compatible-endpoint/v1'"
  log ""

  case "$profile" in
    "$wecomAIProfileName")
      log "  export WECOM_TOKEN='replace-with-your-token'"
      log "  export WECOM_ENCODING_AES_KEY='replace-with-your-43-char-key'"
      log "  export WECOM_AI_CALLBACK_PATH='/wecom/ai/callback'"
      log "  # optional:"
      log "  # export WECOM_CORP_ID='ww1234567890'"
      log "  # export WECOM_AGENT_ID='1000002'"
      log "  # export WECOM_BOT_NAME='OpenClaw'"
      log "  # export WECOM_AIBOTID='replace-with-your-aibotid'"
      ;;
    "$wecomAIWebSocketProfileName")
      log "  export WECOM_STREAM_BOT_ID='replace-with-your-aibotid'"
      log "  export WECOM_STREAM_SECRET='replace-with-your-aibot-secret'"
      log "  # optional:"
      log "  # export WECOM_TOKEN='replace-with-your-token'"
      log "  # export WECOM_ENCODING_AES_KEY='replace-with-your-43-char-key'"
      log "  # export WECOM_CORP_ID='ww1234567890'"
      log "  # export WECOM_AGENT_ID='1000002'"
      log "  # export WECOM_BOT_NAME='OpenClaw'"
      log "  # export WECOM_STREAM_WS_URL='wss://openws.work.weixin.qq.com'"
      ;;
    "$wecomNotificationProfileName")
      log "  export WECOM_TOKEN='replace-with-your-token'"
      log "  export WECOM_ENCODING_AES_KEY='replace-with-your-43-char-key'"
      log \
        "  export WECOM_NOTIFICATION_CALLBACK_PATH='/wecom/notification/callback'"
      log \
        "  export WECOM_WEBHOOK_URL='https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=...'"
      log "  # optional:"
      log "  # export WECOM_CORP_ID='ww1234567890'"
      log "  # export WECOM_AGENT_ID='1000002'"
      log "  # export WECOM_BOT_NAME='OpenClaw'"
      ;;
  esac

  log "  source ~/.bashrc"
}

print_deps_hint() {
  local bin_dir="$1"
  local config_dir="$2"
  local deps_profile="$3"

  log ""
  log "Optional bundled skill deps:"
  log "  Inspect:"
  log "  ${bin_dir}/${binaryName} inspect deps \\"
  log "    --state-dir \"${config_dir}\" \\"
  log "    --bundled"
  log "  Bootstrap bundled skill deps:"
  log "  ${bin_dir}/${binaryName} bootstrap deps \\"
  log "    --state-dir \"${config_dir}\" \\"
  log "    --bundled \\"
  log "    --profile \"${deps_profile}\" \\"
  log "    --apply"
  log ""
  log "This path only plans safe system packages and managed Python deps."
  log "Some skills still keep browser runtimes, global npm installs, or"
  log "credentials as manual setup."
  log "On Linux, system package steps may require root privileges."
}

bootstrap_deps() {
  local bin_dir="$1"
  local config_dir="$2"
  local deps_profile="$3"

  log ""
  log "Bootstrapping bundled skill deps (${deps_profile})..."
  "${bin_dir}/${binaryName}" bootstrap deps \
    --state-dir "$config_dir" \
    --bundled \
    --profile "$deps_profile" \
    --apply
}

main() {
  local version=""
  local channel="$defaultReleaseChannel"
  local profile="$DEFAULT_PROFILE"
  local base_url="$DEFAULT_BASE_URL"
  local force_config="false"
  local bootstrap_optional_deps="false"
  local deps_profile="$DEFAULT_DEPS_PROFILE"
  local bin_dir="${HOME}/${DEFAULT_BIN_SUBDIR}"
  local config_dir="${HOME}/${DEFAULT_CONFIG_SUBDIR}"

  while [ "$#" -gt 0 ]; do
    case "$1" in
      --version)
        version="${2:-}"
        shift 2
        ;;
      --channel)
        channel="${2:-}"
        shift 2
        ;;
      --profile)
        profile="${2:-}"
        shift 2
        ;;
      --base-url)
        base_url="${2:-}"
        shift 2
        ;;
      --bin-dir)
        bin_dir="${2:-}"
        shift 2
        ;;
      --config-dir)
        config_dir="${2:-}"
        shift 2
        ;;
      -f | --force-config)
        force_config="true"
        shift
        ;;
      --bootstrap-deps)
        bootstrap_optional_deps="true"
        shift
        ;;
      --deps-profile)
        deps_profile="${2:-}"
        shift 2
        ;;
      -h | --help)
        usage
        exit 0
        ;;
      *)
        die "unknown option: $1"
        ;;
    esac
  done

  [ -n "$deps_profile" ] || die "empty dependency profile"

  select_download_client
  require_cmd tar

  trap cleanup EXIT INT TERM

  local os arch resolved_version resolved_channel
  local release_url archive_url checksums_url
  local unpack_dir archive_path checksums_path
  local archive_name selected_profile_path
  local package_root metadata_file
  local package_version sqlite_support sqlitevec_support
  local binary_path legacy_binary_path deprecated_launcher_path
  local skills_root bundled_skills_dir local_skills_dir

  base_url="$(trim_trailing_slash "$base_url")"
  resolved_channel="$(normalize_channel "$channel")"
  resolved_version="$(resolve_version \
    "$version" \
    "$base_url" \
    "$resolved_channel")"
  [ -n "$resolved_version" ] || die "empty release version"

  os="$(detect_os)"
  arch="$(detect_arch)"
  archive_name="${binaryName}-${resolved_version}-${os}-${arch}.tar.gz"
  release_url="${base_url}/${releasesDirName}/${resolved_version}"
  archive_url="${release_url}/${archive_name}"
  checksums_url="${release_url}/${checksumsFileName}"

  TEMP_ROOT="$(mktemp -d)"
  unpack_dir="${TEMP_ROOT}/unpack"
  archive_path="${TEMP_ROOT}/${archive_name}"
  checksums_path="${TEMP_ROOT}/${checksumsFileName}"
  mkdir -p "$unpack_dir"

  download_file "$checksums_url" "$checksums_path"
  download_file "$archive_url" "$archive_path"
  verify_checksum "$checksums_path" "$archive_path"

  tar -xzf "$archive_path" -C "$unpack_dir"
  package_root="${unpack_dir}/${packageRootName}"
  [ -d "$package_root" ] || die "invalid package layout"

  mkdir -p "$bin_dir"
  mkdir -p "${config_dir}/${PROFILE_DIR_NAME}"
  skills_root="${config_dir}/${skillsDirName}"
  bundled_skills_dir="${skills_root}/${bundledSkillsDirName}"
  local_skills_dir="${skills_root}/${localSkillsDirName}"
  mkdir -p "$local_skills_dir"

  binary_path="${bin_dir}/${binaryName}"
  legacy_binary_path="${bin_dir}/${legacyBinaryName}"
  deprecated_launcher_path="${bin_dir}/${deprecatedLauncherName}"
  install_file 0755 \
    "${package_root}/bin/${binaryName}" \
    "$binary_path"
  cleanup_legacy_binary "$binary_path" "$legacy_binary_path"
  cleanup_deprecated_launcher "$deprecated_launcher_path"

  install_file 0644 \
    "${package_root}/config/trpc_go.yaml" \
    "${config_dir}/trpc_go.yaml.tmp"
  install_file 0644 \
    "${package_root}/config/openclaw.mock.yaml" \
    "${config_dir}/${PROFILE_DIR_NAME}/openclaw.mock.yaml"
  install_file 0644 \
    "${package_root}/config/openclaw.wecom.ai.yaml" \
    "${config_dir}/${PROFILE_DIR_NAME}/openclaw.wecom.ai.yaml"
  install_file 0644 \
    "${package_root}/config/openclaw.wecom.ai.websocket.yaml" \
    "${config_dir}/${PROFILE_DIR_NAME}/openclaw.wecom.ai.websocket.yaml"
  install_file 0644 \
    "${package_root}/config/openclaw.wecom.notification.yaml" \
    "${config_dir}/${PROFILE_DIR_NAME}/openclaw.wecom.notification.yaml"
  install_file 0644 \
    "${package_root}/config/openclaw.weixin.yaml" \
    "${config_dir}/${PROFILE_DIR_NAME}/openclaw.weixin.yaml"

  if [ -d "${package_root}/${skillsDirName}" ]; then
    rm -rf "$bundled_skills_dir"
    sync_dir_contents \
      "${package_root}/${skillsDirName}" \
      "$bundled_skills_dir"
  else
    warn "package does not contain bundled skills; skip skills install"
  fi

  if [ "$force_config" = "true" ] || \
    [ ! -f "${config_dir}/trpc_go.yaml" ]; then
    mv "${config_dir}/trpc_go.yaml.tmp" "${config_dir}/trpc_go.yaml"
  else
    rm -f "${config_dir}/trpc_go.yaml.tmp"
  fi

  selected_profile_path="${config_dir}/${PROFILE_DIR_NAME}/$(profile_file_name "$profile")"
  if [ "$force_config" = "true" ] || \
    [ ! -f "${config_dir}/openclaw.yaml" ]; then
    cp "$selected_profile_path" "${config_dir}/openclaw.yaml"
  fi

  metadata_file="${package_root}/metadata.env"
  package_version="$resolved_version"
  sqlite_support="unknown"
  sqlitevec_support="unknown"
  if [ -f "$metadata_file" ]; then
    # shellcheck disable=SC1090
    . "$metadata_file"
    package_version="${PACKAGE_VERSION:-$resolved_version}"
    sqlite_support="${SQLITE_MEMORY_BACKEND:-unknown}"
    sqlitevec_support="${SQLITEVEC_MEMORY_BACKEND:-unknown}"
  fi

  log ""
  log "trpc-claw installed."
  log "Package: ${package_version}"
  log "Channel: ${resolved_channel}"
  log "Binary: ${binary_path}"
  log "Profile: ${profile}"
  log "Config: ${config_dir}/openclaw.yaml"
  log "tRPC:   ${config_dir}/trpc_go.yaml"
  log "SQLite: ${sqlite_support}"
  log "SQLiteVec: ${sqlitevec_support}"
  log "Skills: ${bundled_skills_dir}"
  log "Local:  ${local_skills_dir}"
  log "Profiles:"
  log "  ${config_dir}/${PROFILE_DIR_NAME}/openclaw.mock.yaml"
  log "  ${config_dir}/${PROFILE_DIR_NAME}/openclaw.wecom.ai.yaml"
  log "  ${config_dir}/${PROFILE_DIR_NAME}/openclaw.wecom.ai.websocket.yaml"
  log "  ${config_dir}/${PROFILE_DIR_NAME}/openclaw.wecom.notification.yaml"
  log "  ${config_dir}/${PROFILE_DIR_NAME}/openclaw.weixin.yaml"
  log ""
  log "Run:"
  log "  ${binary_path}"
  log ""
  log "Switch to WeCom AI profile:"
  log "  cp ${config_dir}/${PROFILE_DIR_NAME}/openclaw.wecom.ai.yaml \\"
  log "    ${config_dir}/openclaw.yaml"
  log "  vim ${config_dir}/openclaw.yaml"
  log "Switch to WeCom AI websocket profile:"
  log \
    "  cp ${config_dir}/${PROFILE_DIR_NAME}/openclaw.wecom.ai.websocket.yaml \\"
  log "    ${config_dir}/openclaw.yaml"
  log "  vim ${config_dir}/openclaw.yaml"
  log "Switch to Weixin profile:"
  log "  cp ${config_dir}/${PROFILE_DIR_NAME}/openclaw.weixin.yaml \\"
  log "    ${config_dir}/openclaw.yaml"
  log "  vim ${config_dir}/openclaw.yaml"
  log ""
  log "Add your own skills under:"
  log "  ${local_skills_dir}"

  case "$profile" in
    "$wecomAIProfileName" | "$wecomAIWebSocketProfileName" | \
      "$wecomNotificationProfileName")
      print_wecom_env_hint "$profile"
      ;;
  esac

  if [ "$sqlite_support" != "$sqliteEnabledValue" ]; then
    log ""
    log "Note: this package was built without sqlite support."
  fi
  if [ "$sqlitevec_support" != "$sqliteEnabledValue" ]; then
    log ""
    log "Note: this package was built without sqlitevec support."
  fi

  if [ "$bootstrap_optional_deps" = "true" ]; then
    bootstrap_deps "$bin_dir" "$config_dir" "$deps_profile"
  else
    print_deps_hint "$bin_dir" "$config_dir" "$deps_profile"
  fi

  print_path_hint "$bin_dir"
}

main "$@"
