#!/usr/bin/env bash
set -euo pipefail

# Sample platform-managed supervisor for a long-running trpc-claw
# deployment.
#
# Core model:
# - the platform keeps `start.sh` as the outer entrypoint
# - this script always launches the stable `trpc-claw` binary path
# - `trpc-claw` writes lifecycle intent files and exits with a dedicated
#   code when it wants a graceful restart or upgrade
# - this script reads the intent, performs the platform-side action, and
#   then starts the next `trpc-claw` child
#
# Default flow:
# 1. create the workspace and `cd` into it
# 2. before each child start, optionally source a platform-managed env
#    file
# 3. install `trpc-claw` on first boot if the binary is missing
# 4. source the prestart hook before each child start
# 5. launch `trpc-claw` and wait for it to exit
# 6. if the exit code is the lifecycle code, read `intent.env`,
#    restart or upgrade the stable binary, and loop
#
# Safe extension points:
# - export platform secrets before the child start
# - set `TRPC_CLAW_PRESTART_HOOK` to run a shell hook before each child
# - override `TRPC_CLAW_RELEASE_BASE_URL`,
#   `TRPC_CLAW_RELEASE_CHANNEL`, or `TRPC_CLAW_INITIAL_VERSION`
# - replace `handle_lifecycle_intent` only if the platform wants to own
#   version policy itself
#
# Important:
# - keep exactly one supervisor layer; this script is already the outer
#   loop
# - keep `trpc-claw` as the only business binary path; do not add a
#   second wrapper around it

readonly defaultBaseURL="https://mirrors.tencent.com/repository/"\
"generic/trpc-agent-go/trpc-claw"
readonly defaultInstallProfile="wecom-ai-websocket"
readonly defaultWorkspaceDir="/data/cic/workspace"
readonly defaultStateSubpath=".trpc-agent-go/openclaw"
readonly defaultBinSubpath=".local/bin"
readonly defaultConfigFileName="openclaw.yaml"
readonly defaultHookSubpath=".trpc-agent-go/openclaw/hooks/prestart.sh"
readonly installScriptName="install.sh"
readonly latestDirName="latest"
readonly previewDirName="preview"
readonly defaultReleaseChannel="$latestDirName"
readonly binaryName="trpc-claw"
readonly lifecycleDirRelPath="runtime/lifecycle"
readonly lifecycleExitCode="75"
readonly intentEnvFileName="intent.env"
readonly intentJSONFileName="intent.json"

TEMP_ROOT=""
DOWNLOAD_CLIENT=""
CHILD_PID=""
SHUTTING_DOWN="false"

log() {
  printf '%s\n' "$*"
}

warn() {
  printf 'warning: %s\n' "$*" >&2
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

cleanup() {
  if [ -n "$TEMP_ROOT" ] && [ -d "$TEMP_ROOT" ]; then
    rm -rf "$TEMP_ROOT"
  fi
}

ensure_temp_root() {
  if [ -n "$TEMP_ROOT" ] && [ -d "$TEMP_ROOT" ]; then
    return
  fi
  TEMP_ROOT="$(mktemp -d)"
}

has_cmd() {
  command -v "$1" >/dev/null 2>&1
}

ensure_path_contains() {
  local dir="$1"

  case ":${PATH}:" in
    *":${dir}:"*)
      return
      ;;
  esac
  export PATH="${dir}:${PATH}"
}

trim_trailing_slash() {
  local value="$1"

  printf '%s' "${value%/}"
}

contains_arg() {
  local needle="$1"
  shift
  local arg=""

  for arg in "$@"; do
    case "$arg" in
      "$needle" | "${needle}="*)
        return 0
        ;;
    esac
  done
  return 1
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

user_home() {
  if [ -n "${HOME:-}" ]; then
    printf '%s' "$HOME"
    return
  fi
  die "HOME is not set"
}

script_dir() {
  cd "$(dirname "${BASH_SOURCE[0]}")" && pwd
}

bin_dir() {
  if [ -n "${TRPC_CLAW_BIN_DIR:-}" ]; then
    printf '%s' "$TRPC_CLAW_BIN_DIR"
    return
  fi
  printf '%s/%s' "$(user_home)" "$defaultBinSubpath"
}

binary_path() {
  printf '%s/%s' "$(bin_dir)" "$binaryName"
}

state_dir() {
  if [ -n "${TRPC_CLAW_STATE_DIR:-}" ]; then
    printf '%s' "$TRPC_CLAW_STATE_DIR"
    return
  fi
  printf '%s/%s' "$(user_home)" "$defaultStateSubpath"
}

workspace_dir() {
  if [ -n "${TRPC_CLAW_WORKSPACE_DIR:-}" ]; then
    printf '%s' "$TRPC_CLAW_WORKSPACE_DIR"
    return
  fi
  printf '%s' "$defaultWorkspaceDir"
}

config_path() {
  if [ -n "${TRPC_CLAW_CONFIG_PATH:-}" ]; then
    printf '%s' "$TRPC_CLAW_CONFIG_PATH"
    return
  fi
  printf '%s/%s' "$(state_dir)" "$defaultConfigFileName"
}

hook_path() {
  if [ -n "${TRPC_CLAW_PRESTART_HOOK:-}" ]; then
    printf '%s' "$TRPC_CLAW_PRESTART_HOOK"
    return
  fi
  printf '%s/%s' "$(user_home)" "$defaultHookSubpath"
}

release_base_url() {
  if [ -n "${TRPC_CLAW_RELEASE_BASE_URL:-}" ]; then
    printf '%s' "$TRPC_CLAW_RELEASE_BASE_URL"
    return
  fi
  printf '%s' "$defaultBaseURL"
}

normalize_release_channel() {
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

release_channel() {
  normalize_release_channel "${TRPC_CLAW_RELEASE_CHANNEL:-$defaultReleaseChannel}"
}

install_script_rel_path() {
  local channel="$1"

  printf '%s/%s' "$channel" "$installScriptName"
}

install_profile() {
  if [ -n "${TRPC_CLAW_INSTALL_PROFILE:-}" ]; then
    printf '%s' "$TRPC_CLAW_INSTALL_PROFILE"
    return
  fi
  printf '%s' "$defaultInstallProfile"
}

export_runtime_env_defaults() {
  export TRPC_CLAW_BIN_DIR="$(bin_dir)"
  export TRPC_CLAW_STATE_DIR="$(state_dir)"
  export TRPC_CLAW_WORKSPACE_DIR="$(workspace_dir)"
  export TRPC_CLAW_CONFIG_PATH="$(config_path)"
  export TRPC_CLAW_RELEASE_BASE_URL="$(release_base_url)"
  export TRPC_CLAW_RELEASE_CHANNEL="$(release_channel)"

  if [ -n "$(install_profile)" ]; then
    export TRPC_CLAW_INSTALL_PROFILE="$(install_profile)"
  fi
}

lifecycle_dir() {
  printf '%s/%s' "$(state_dir)" "$lifecycleDirRelPath"
}

intent_env_path() {
  printf '%s/%s' "$(lifecycle_dir)" "$intentEnvFileName"
}

local_install_script_path() {
  local path=""

  path="$(script_dir)/${installScriptName}"
  if [ -f "$path" ]; then
    printf '%s' "$path"
    return
  fi
  printf ''
}

download_install_script() {
  local channel="$1"
  local output=""

  select_download_client
  ensure_temp_root
  output="${TEMP_ROOT}/${installScriptName}"
  download_file \
    "$(trim_trailing_slash "$(release_base_url)")/$(install_script_rel_path "$channel")" \
    "$output"
  chmod 0755 "$output"
  printf '%s' "$output"
}

select_install_script() {
  local channel="$1"
  local local_path=""

  local_path="$(local_install_script_path)"
  if [ -n "$local_path" ]; then
    printf '%s' "$local_path"
    return
  fi
  download_install_script "$channel"
}

source_platform_env_file() {
  local env_file="${TRPC_CLAW_ENV_FILE:-}"

  if [ -z "$env_file" ]; then
    return
  fi
  if [ ! -f "$env_file" ]; then
    warn "skip missing env file: ${env_file}"
    return
  fi

  log "sourcing env file: ${env_file}"
  set -a
  # shellcheck disable=SC1090
  . "$env_file"
  set +a
}

run_prestart_hook() {
  local path=""

  path="$(hook_path)"
  if [ ! -f "$path" ]; then
    return
  fi

  log "sourcing prestart hook: ${path}"
  # shellcheck disable=SC1090
  . "$path"
}

ensure_workspace() {
  local workspace=""

  workspace="$(workspace_dir)"
  mkdir -p "$workspace"
  cd "$workspace"
}

install_runtime() {
  local install_script=""
  local channel=""
  local args=()

  mkdir -p "$(bin_dir)"
  mkdir -p "$(state_dir)"
  channel="$(release_channel)"
  install_script="$(select_install_script "$channel")"
  args=(
    --base-url "$(release_base_url)"
    --channel "$channel"
    --bin-dir "$(bin_dir)"
    --config-dir "$(state_dir)"
  )
  if [ -n "${TRPC_CLAW_INITIAL_VERSION:-}" ]; then
    args+=(--version "${TRPC_CLAW_INITIAL_VERSION}")
  fi
  if [ -n "$(install_profile)" ]; then
    args+=(--profile "$(install_profile)")
  fi

  log "installing trpc-claw runtime"
  "$install_script" "${args[@]}"
}

ensure_runtime_installed() {
  if [ -x "$(binary_path)" ]; then
    return
  fi
  install_runtime
}

upgrade_runtime() {
  local target_version="$1"
  local target_channel="$2"
  local install_script=""
  local channel=""
  local args=()

  if [ -n "$target_channel" ]; then
    channel="$(normalize_release_channel "$target_channel")"
  else
    channel="$(release_channel)"
  fi
  install_script="$(select_install_script "$channel")"
  args=(
    --base-url "$(release_base_url)"
    --channel "$channel"
    --bin-dir "$(bin_dir)"
    --config-dir "$(state_dir)"
  )
  if [ -n "$target_version" ]; then
    args+=(--version "$target_version")
  fi

  log "upgrading trpc-claw ${target_version:-latest}"
  "$install_script" "${args[@]}"
}

clear_lifecycle_files() {
  rm -f "$(lifecycle_dir)/${intentEnvFileName}"
  rm -f "$(lifecycle_dir)/${intentJSONFileName}"
}

consume_lifecycle_intent() {
  local env_path=""

  env_path="$(intent_env_path)"
  [ -f "$env_path" ] || die "missing lifecycle intent: ${env_path}"
  # shellcheck disable=SC1090
  . "$env_path"
}

handle_lifecycle_intent() {
  local action="${TRPC_CLAW_LIFECYCLE_ACTION:-}"
  local mode="${TRPC_CLAW_LIFECYCLE_MODE:-}"
  local target_version="${TRPC_CLAW_LIFECYCLE_TARGET_VERSION:-}"
  local target_channel="${TRPC_CLAW_LIFECYCLE_TARGET_CHANNEL:-}"

  case "$action" in
    restart)
      log "handling lifecycle intent: ${mode} restart"
      ;;
    upgrade)
      log "handling lifecycle intent: ${mode} upgrade"
      upgrade_runtime "$target_version" "$target_channel"
      ;;
    *)
      die "unsupported lifecycle action: ${action}"
      ;;
  esac
}

forward_signal() {
  local signal="$1"

  SHUTTING_DOWN="true"
  if [ -n "$CHILD_PID" ] && kill -0 "$CHILD_PID" >/dev/null 2>&1; then
    kill "-${signal}" "$CHILD_PID" >/dev/null 2>&1 || true
  fi
}

main() {
  local exit_code=""
  local args=()

  trap cleanup EXIT
  trap 'forward_signal TERM' TERM
  trap 'forward_signal INT' INT

  source_platform_env_file
  ensure_workspace

  while true; do
    args=()
    source_platform_env_file
    export_runtime_env_defaults
    ensure_path_contains "$(bin_dir)"
    ensure_runtime_installed
    mkdir -p "$(lifecycle_dir)"
    clear_lifecycle_files
    run_prestart_hook
    export_runtime_env_defaults

    # Keep the child config explicit by default. If the platform already
    # passes `-config ...`, the example will not inject a duplicate flag.
    if [ -f "$(config_path)" ] && ! contains_arg "-config" "$@"; then
      args+=(-config "$(config_path)")
    fi
    if [ -n "${TRPC_CLAW_MODE:-}" ] && \
      ! contains_arg "-mode" "$@"; then
      args+=(-mode "${TRPC_CLAW_MODE}")
    fi

    log "starting trpc-claw via $(binary_path)"
    "$(binary_path)" "${args[@]}" "$@" &
    CHILD_PID="$!"
    if wait "$CHILD_PID"; then
      exit_code="0"
    else
      exit_code="$?"
    fi
    CHILD_PID=""

    if [ "$SHUTTING_DOWN" = "true" ]; then
      exit "$exit_code"
    fi
    if [ "$exit_code" != "$lifecycleExitCode" ]; then
      exit "$exit_code"
    fi

    consume_lifecycle_intent
    handle_lifecycle_intent
    clear_lifecycle_files
  done
}

main "$@"
