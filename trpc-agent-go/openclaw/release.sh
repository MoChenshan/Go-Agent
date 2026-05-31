#!/usr/bin/env bash
set -euo pipefail

readonly DEFAULT_TARGETS="linux/amd64,linux/arm64,darwin/amd64,darwin/arm64"
readonly DEFAULT_BUILDER_IMAGE="ghcr.io/crazy-max/xgo:latest"
readonly DEFAULT_MIRROR_HOST="https://mirrors.tencent.com"
readonly DEFAULT_MIRROR_REPO_NAME="trpc-agent-go"
readonly DEFAULT_MIRROR_PATH_PREFIX="trpc-claw"
readonly DEFAULT_USERNAME_ENV="TENCENT_USERNAME"
readonly DEFAULT_TOKEN_ENV="TENCENT_TOKEN"
readonly DEFAULT_MAX_BUILD_JOBS="2"

readonly DIST_DIR_NAME="dist"
readonly PACKAGE_ROOT_NAME="trpc-claw"
readonly INSTALL_DOC_NAME="INSTALL.md"
readonly CHANGELOG_DOC_NAME="CHANGELOG.md"
readonly DEFAULT_GO_BUILD_TAGS="openclaw_sqlitevec"
readonly DEFAULT_CACHE_DIR_NAME=".release-cache"
readonly DEFAULT_BUILDER_GO_ROOT="/usr/local/go"
readonly releaseIndexFileName="releases.json"
readonly minRuntimeTargetVersion="v0.0.48"

readonly releasesDirName="releases"
readonly latestDirName="latest"
readonly previewDirName="preview"
readonly defaultReleaseChannel="$latestDirName"
readonly versionFileName="VERSION"
readonly manifestFileName="manifest.env"
readonly checksumsFileName="checksums.txt"
readonly installScriptName="install.sh"
readonly startScriptName="start.sh"

readonly targetLinuxAMD64="linux/amd64"
readonly targetLinuxARM64="linux/arm64"
readonly targetDarwinAMD64="darwin/amd64"
readonly targetDarwinARM64="darwin/arm64"

readonly compilerLinuxAMD64="x86_64-linux-gnu-gcc"
readonly compilerLinuxARM64="aarch64-linux-gnu-gcc"
readonly compilerDarwinAMD64="o64-clang"
readonly compilerDarwinARM64="oa64-clang"

readonly cxxLinuxAMD64="x86_64-linux-gnu-g++"
readonly cxxLinuxARM64="aarch64-linux-gnu-g++"
readonly cxxDarwinAMD64="o64-clang++"
readonly cxxDarwinARM64="oa64-clang++"

readonly outputBinaryName="trpc-claw"
readonly sqliteEnabledValue="enabled"
readonly sqliteSymbolPattern="sqlite3_open_v2"
readonly permissionAdmin="admin"
readonly openClawConfigEnvName="OPENCLAW_CONFIG"
readonly stateDirConfigKey="state_dir"

BUILD_BUILDER_IMAGE=""
BUILD_BUILDER_GO_ROOT=""
BUILD_GO_MOD_CACHE=""
BUILD_GO_BUILD_CACHE=""
BUILD_GO_VERSION=""
BUILD_HOST_GOOS=""
BUILD_HOST_GOARCH=""
BUILD_SQLITE_INCLUDE_DIR=""
BUILD_CACHE_ROOT=""

usage() {
  cat <<'EOF'
Build and publish the internal trpc-claw distribution.

Usage:
  release.sh build --version <version> [options]
  release.sh publish --version <version> [options]
  release.sh release --version <version> [options]

Commands:
  build
      Build release archives under openclaw/dist/<version>.

  publish
      Upload install.sh and build artifacts to Tencent Mirrors generic
      repo paths:
        <prefix>/releases/<version>/
        <prefix>/<channel>/

  release
      Run build, then publish.

Options:
  --version <version>         Release version, for example v0.0.11.
  --targets <list>            Comma-separated targets.
                              Default:
                              linux/amd64,linux/arm64,darwin/amd64,
                              darwin/arm64
  --builder-image <image>     Docker image used for CGO cross-builds.
  --jobs <count>              Parallel target build jobs.
                              Default: auto, capped at 2.
  --cache-dir <path>          Persistent build cache directory.
                              Default:
                              openclaw/dist/.release-cache
  --mirror-host <url>         Mirrors host.
                              Default: https://mirrors.tencent.com
  --repo-name <name>          Generic repo name.
                              Default: trpc-agent-go
  --path-prefix <path>        Path prefix inside the repo.
                              Default: trpc-claw
  --channel <latest|preview>  Publish channel.
                              Default: latest
  --username-env <name>       Env var that stores the mirrors username.
                              Default: TENCENT_USERNAME
  --token-env <name>          Env var that stores the mirrors token.
                              Default: TENCENT_TOKEN
  -h, --help                  Show help.
EOF
}

log() {
  printf '%s\n' "$*"
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    die "missing required command: $1"
  fi
}

repo_root() {
  git -C "$(dirname "${BASH_SOURCE[0]}")" \
    rev-parse --show-toplevel
}

module_dir() {
  printf '%s/openclaw' "$(repo_root)"
}

pick_go_bin() {
  local candidate=""

  candidate="${OPENCLAW_GO_BIN:-}"
  if [ -n "$candidate" ] && [ -x "$candidate" ]; then
    printf '%s' "$candidate"
    return
  fi
  command -v go
}

pick_builder_go_root() {
  printf '%s' "${OPENCLAW_BUILDER_GOROOT:-$DEFAULT_BUILDER_GO_ROOT}"
}

trim_trailing_slash() {
  local value="$1"

  printf '%s' "${value%/}"
}

absolute_path() {
  local value="$1"
  local dir base

  case "$value" in
    /*)
      printf '%s' "$value"
      return
      ;;
  esac
  dir="$(dirname "$value")"
  base="$(basename "$value")"
  printf '%s/%s' "$(cd "$dir" && pwd)" "$base"
}

release_cache_dir() {
  printf '%s/%s/%s' \
    "$(module_dir)" \
    "$DIST_DIR_NAME" \
    "$DEFAULT_CACHE_DIR_NAME"
}

epoch_now() {
  date +%s
}

format_duration_seconds() {
  local total="${1:-0}"
  local hours minutes seconds

  if ! [[ "$total" =~ ^[0-9]+$ ]]; then
    total=0
  fi
  hours=$((total / 3600))
  minutes=$(((total % 3600) / 60))
  seconds=$((total % 60))
  if [ "$hours" -gt 0 ]; then
    printf '%dh%02dm%02ds' "$hours" "$minutes" "$seconds"
    return
  fi
  if [ "$minutes" -gt 0 ]; then
    printf '%dm%02ds' "$minutes" "$seconds"
    return
  fi
  printf '%ds' "$seconds"
}

is_positive_int() {
  [[ "${1:-}" =~ ^[1-9][0-9]*$ ]]
}

host_cpu_count() {
  local count=""

  if command -v nproc >/dev/null 2>&1; then
    count="$(nproc)"
  elif command -v getconf >/dev/null 2>&1; then
    count="$(getconf _NPROCESSORS_ONLN 2>/dev/null || true)"
  elif command -v sysctl >/dev/null 2>&1; then
    count="$(sysctl -n hw.ncpu 2>/dev/null || true)"
  fi
  if ! is_positive_int "$count"; then
    count=1
  fi
  printf '%s' "$count"
}

recommended_build_jobs() {
  local target_count="$1"
  local cpu_count jobs

  cpu_count="$(host_cpu_count)"
  jobs="$cpu_count"
  if [ "$jobs" -gt "$DEFAULT_MAX_BUILD_JOBS" ]; then
    jobs="$DEFAULT_MAX_BUILD_JOBS"
  fi
  if [ "$jobs" -gt "$target_count" ]; then
    jobs="$target_count"
  fi
  if [ "$jobs" -lt 1 ]; then
    jobs=1
  fi
  printf '%s' "$jobs"
}

prepare_build_cache_dirs() {
  local cache_root="$1"

  mkdir -p "$cache_root"
  mkdir -p "${cache_root}/go-build"
  mkdir -p "${cache_root}/sqlite/include"
}

dist_dir() {
  local version="$1"

  printf '%s/%s/%s' \
    "$(module_dir)" \
    "$DIST_DIR_NAME" \
    "$version"
}

archive_name() {
  local version="$1"
  local goos="$2"
  local goarch="$3"

  printf '%s-%s-%s-%s.tar.gz' \
    "$outputBinaryName" "$version" "$goos" "$goarch"
}

target_slug() {
  printf '%s' "$1" | tr '/' '-'
}

write_metadata() {
  local output="$1"
  local version="$2"
  local goos="$3"
  local goarch="$4"
  local cgo_enabled="$5"
  local go_version="$6"
  local builder_image="$7"
  local commit

  commit="$(git -C "$(repo_root)" rev-parse HEAD)"
  cat >"$output" <<EOF
PACKAGE_VERSION='${version}'
PACKAGE_GOOS='${goos}'
PACKAGE_GOARCH='${goarch}'
CGO_ENABLED='${cgo_enabled}'
SQLITE_MEMORY_BACKEND='${sqliteEnabledValue}'
SQLITEVEC_MEMORY_BACKEND='${sqliteEnabledValue}'
PACKAGE_GO_VERSION='${go_version}'
BUILDER_IMAGE='${builder_image}'
SOURCE_COMMIT='${commit}'
EOF
}

compiler_for_target() {
  case "$1" in
    "$targetLinuxAMD64")
      printf '%s' "$compilerLinuxAMD64"
      ;;
    "$targetLinuxARM64")
      printf '%s' "$compilerLinuxARM64"
      ;;
    "$targetDarwinAMD64")
      printf '%s' "$compilerDarwinAMD64"
      ;;
    "$targetDarwinARM64")
      printf '%s' "$compilerDarwinARM64"
      ;;
    *)
      die "unsupported target: $1"
      ;;
  esac
}

cxx_for_target() {
  case "$1" in
    "$targetLinuxAMD64")
      printf '%s' "$cxxLinuxAMD64"
      ;;
    "$targetLinuxARM64")
      printf '%s' "$cxxLinuxARM64"
      ;;
    "$targetDarwinAMD64")
      printf '%s' "$cxxDarwinAMD64"
      ;;
    "$targetDarwinARM64")
      printf '%s' "$cxxDarwinARM64"
      ;;
    *)
      die "unsupported target: $1"
      ;;
  esac
}

sync_module_cache() {
  local go_bin="$1"
  local started_at elapsed

  log "sync module cache with host Go SDK"
  started_at="$(epoch_now)"
  (
    cd "$(module_dir)"
    "$go_bin" mod download
  )
  elapsed=$(( $(epoch_now) - started_at ))
  log "synced module cache in $(format_duration_seconds "$elapsed")"
}

resolve_sqlite_module_dir() {
  local go_bin="$1"

  (
    cd "$(module_dir)"
    "$go_bin" list -m -f '{{.Dir}}' github.com/mattn/go-sqlite3
  )
}

prepare_sqlite_headers() {
  local go_bin="$1"
  local sqlite_module_dir=""

  sqlite_module_dir="$(resolve_sqlite_module_dir "$go_bin")"
  [ -n "$sqlite_module_dir" ] || \
    die "failed to resolve github.com/mattn/go-sqlite3"
  [ -d "$sqlite_module_dir" ] || \
    die "sqlite module dir does not exist: ${sqlite_module_dir}"

  mkdir -p "$BUILD_SQLITE_INCLUDE_DIR"
  install -m 0644 "${sqlite_module_dir}/sqlite3-binding.h" \
    "${BUILD_SQLITE_INCLUDE_DIR}/sqlite3.h"
  install -m 0644 "${sqlite_module_dir}/sqlite3ext.h" \
    "${BUILD_SQLITE_INCLUDE_DIR}/sqlite3ext.h"
}

verify_sqlite_symbols() {
  local binary_path="$1"

  if ! grep -q "$sqliteSymbolPattern" < <(strings "$binary_path"); then
    die "sqlite symbols missing in ${binary_path}"
  fi
}

smoke_test_host_binary() {
  local binary_path="$1"
  local temp_home config_path state_dir output

  temp_home="$(mktemp -d)"
  state_dir="${temp_home}/state"
  config_path="${temp_home}/openclaw.yaml"
  mkdir -p "$state_dir"
  printf '%s: %s\n' "$stateDirConfigKey" "$state_dir" >"$config_path"
  if ! output="$(HOME="$temp_home" \
    env "${openClawConfigEnvName}=${config_path}" \
    "$binary_path" inspect plugins 2>&1)"; then
    rm -rf "$temp_home"
    die "host smoke test failed: ${output}"
  fi
  if ! printf '%s\n' "$output" | grep -qx -- '- sqlite'; then
    rm -rf "$temp_home"
    die "sqlite backend missing in host smoke test"
  fi
  if ! printf '%s\n' "$output" | grep -qx -- '- sqlitevec'; then
    rm -rf "$temp_home"
    die "sqlitevec backend missing in host smoke test"
  fi
  rm -rf "$temp_home"
}

copy_dir_contents() {
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

build_target_binary() {
  local version="$1"
  local commit="$2"
  local target="$3"
  local output_dir="$4"
  local goos goarch cc cxx

  goos="${target%/*}"
  goarch="${target#*/}"
  cc="$(compiler_for_target "$target")"
  cxx="$(cxx_for_target "$target")"

  docker run --rm \
    --user "$(id -u):$(id -g)" \
    -v "$(repo_root):/src" \
    -v "${BUILD_GO_MOD_CACHE}:/ext/gomodcache:ro" \
    -v "${BUILD_GO_BUILD_CACHE}:/ext/gocache" \
    -v "${BUILD_SQLITE_INCLUDE_DIR}:/ext/sqlite/include:ro" \
    -v "${output_dir}:/out" \
    -w /src/openclaw \
    -e GOROOT="${BUILD_BUILDER_GO_ROOT}" \
    -e PATH="${BUILD_BUILDER_GO_ROOT}/bin:/osxcross/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" \
    -e GOMODCACHE=/ext/gomodcache \
    -e GOCACHE=/ext/gocache \
    -e CGO_CFLAGS=-I/ext/sqlite/include \
    -e GOPROXY=off \
    -e GOSUMDB=off \
    -e GOFLAGS=-buildvcs=false \
    -e GOTOOLCHAIN=local \
    -e CGO_ENABLED=1 \
    -e GOOS="${goos}" \
    -e GOARCH="${goarch}" \
    -e CC="${cc}" \
    -e CXX="${cxx}" \
    --entrypoint sh \
    "${BUILD_BUILDER_IMAGE}" \
    -lc "go build -trimpath \
      -tags '${DEFAULT_GO_BUILD_TAGS}' \
      -ldflags '-X main.releaseVersion=${version} \
      -X main.buildBaseVersion=${version} \
      -X main.buildCommit=${commit}' \
      -o /out/${outputBinaryName} ./cmd/openclaw"
}

builder_go_version() {
  docker run --rm \
    -e GOROOT="${BUILD_BUILDER_GO_ROOT}" \
    -e PATH="${BUILD_BUILDER_GO_ROOT}/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" \
    --entrypoint sh \
    "${BUILD_BUILDER_IMAGE}" \
    -lc "go version" | awk '{ print $3 }'
}

package_target() {
  local version="$1"
  local target="$2"
  local commit goos goarch archive output_dir channel
  local stage_dir package_dir dist target_id
  local started_at elapsed

  commit="$(git -C "$(repo_root)" rev-parse --short HEAD)"
  goos="${target%/*}"
  goarch="${target#*/}"
  channel="$(release_channel_for_version "$version")"
  target_id="$(target_slug "$target")"
  dist="$(dist_dir "$version")"
  output_dir="${dist}/_build/${target_id}"
  stage_dir="${output_dir}/${PACKAGE_ROOT_NAME}"
  package_dir="${stage_dir}/bin"
  archive="$(archive_name "$version" "$goos" "$goarch")"

  rm -rf "$output_dir"
  mkdir -p "$package_dir"
  mkdir -p "${stage_dir}/config"

  started_at="$(epoch_now)"
  log "build ${target} (CGO_ENABLED=1 via ${BUILD_BUILDER_IMAGE})"
  build_target_binary "$version" "$commit" "$target" "$package_dir"
  verify_sqlite_symbols "${package_dir}/${outputBinaryName}"

  if [ "$goos" = "$BUILD_HOST_GOOS" ] && \
    [ "$goarch" = "$BUILD_HOST_GOARCH" ]; then
    smoke_test_host_binary "${package_dir}/${outputBinaryName}"
  fi

  cp "$(module_dir)/openclaw.yaml" \
    "${stage_dir}/config/openclaw.mock.yaml"
  cp "$(module_dir)/openclaw.wecom.ai.yaml" \
    "${stage_dir}/config/openclaw.wecom.ai.yaml"
  cp "$(module_dir)/openclaw.wecom.ai.websocket.yaml" \
    "${stage_dir}/config/openclaw.wecom.ai.websocket.yaml"
  cp "$(module_dir)/openclaw.wecom.notification.yaml" \
    "${stage_dir}/config/openclaw.wecom.notification.yaml"
  cp "$(module_dir)/openclaw.weixin.yaml" \
    "${stage_dir}/config/openclaw.weixin.yaml"
  cp "$(module_dir)/trpc_go.yaml" \
    "${stage_dir}/config/trpc_go.yaml"
  copy_dir_contents \
    "$(module_dir)/skills" \
    "${stage_dir}/skills"
  cp "$(module_dir)/${INSTALL_DOC_NAME}" \
    "${stage_dir}/README.md"
  write_channel_default_script \
    "$installScriptName" \
    "$channel" \
    "${stage_dir}/${installScriptName}"
  write_channel_default_script \
    "$startScriptName" \
    "$channel" \
    "${stage_dir}/${startScriptName}"
  chmod 0755 \
    "${stage_dir}/${installScriptName}" \
    "${stage_dir}/${startScriptName}"
  write_metadata \
    "${stage_dir}/metadata.env" \
    "$version" \
    "$goos" \
    "$goarch" \
    "1" \
    "$BUILD_GO_VERSION" \
    "$BUILD_BUILDER_IMAGE"

  (
    cd "$output_dir"
    tar -czf "${dist}/${archive}" "${PACKAGE_ROOT_NAME}"
  )
  rm -rf "$output_dir"
  elapsed=$(( $(epoch_now) - started_at ))
  log "finished ${target} in $(format_duration_seconds "$elapsed")"
}

write_checksums() {
  local version="$1"
  local dist checksum_cmd

  dist="$(dist_dir "$version")"
  if command -v sha256sum >/dev/null 2>&1; then
    checksum_cmd="sha256sum"
  elif command -v shasum >/dev/null 2>&1; then
    checksum_cmd="shasum -a 256"
  else
    die "missing sha256sum or shasum"
  fi

  (
    cd "$dist"
    eval "$checksum_cmd" ./*.tar.gz | \
      sed 's# \\./#  #' > "$checksumsFileName"
  )
}

write_manifest() {
  local version="$1"
  local targets="$2"
  local builder_image="$3"
  local go_version="$4"
  local dist commit

  dist="$(dist_dir "$version")"
  commit="$(git -C "$(repo_root)" rev-parse HEAD)"
  cat >"${dist}/${manifestFileName}" <<EOF
VERSION='${version}'
SOURCE_COMMIT='${commit}'
TARGETS='${targets}'
BUILDER_IMAGE='${builder_image}'
GO_VERSION='${go_version}'
EOF
}

run_target_builds() {
  local version="$1"
  local jobs="$2"
  shift 2
  local target=""
  local pid="" failed_pid="" failed_target=""
  local other_pid="" status=0
  local -a batch_pids=()
  local -A batch_targets=()

  for target in "$@"; do
    package_target "$version" "$target" &
    pid="$!"
    batch_pids+=("$pid")
    batch_targets[$pid]="$target"
    if [ "${#batch_pids[@]}" -lt "$jobs" ]; then
      continue
    fi
    failed_pid=""
    for pid in "${batch_pids[@]}"; do
      if wait "$pid"; then
        continue
      fi
      status="$?"
      failed_pid="$pid"
      failed_target="${batch_targets[$pid]}"
      break
    done
    if [ -n "$failed_pid" ]; then
      for other_pid in "${batch_pids[@]}"; do
        if [ "$other_pid" = "$failed_pid" ]; then
          continue
        fi
        kill "$other_pid" >/dev/null 2>&1 || true
      done
      wait >/dev/null 2>&1 || true
      die "build failed for ${failed_target} with exit code ${status}"
    fi
    batch_pids=()
    batch_targets=()
  done
  for pid in "${batch_pids[@]}"; do
    if wait "$pid"; then
      continue
    fi
    status="$?"
    failed_pid="$pid"
    failed_target="${batch_targets[$pid]}"
    for other_pid in "${batch_pids[@]}"; do
      if [ "$other_pid" = "$failed_pid" ]; then
        continue
      fi
      kill "$other_pid" >/dev/null 2>&1 || true
    done
    wait >/dev/null 2>&1 || true
    die "build failed for ${failed_target} with exit code ${status}"
  done
}

build_release() {
  local version="$1"
  local targets="$2"
  local builder_image="$3"
  local jobs="$4"
  local cache_root="$5"
  local go_bin
  local build_started_at build_elapsed
  local -a target_array=()

  [ -n "$version" ] || die "--version is required"
  go_bin="$(pick_go_bin)"
  require_cmd git
  require_cmd docker
  require_cmd strings
  require_cmd tar
  require_cmd "$go_bin"

  sync_module_cache "$go_bin"

  BUILD_BUILDER_IMAGE="$builder_image"
  BUILD_BUILDER_GO_ROOT="$(pick_builder_go_root)"
  cache_root="$(absolute_path "$cache_root")"
  BUILD_GO_MOD_CACHE="$("$go_bin" env GOMODCACHE)"
  BUILD_CACHE_ROOT="$cache_root"
  prepare_build_cache_dirs "$BUILD_CACHE_ROOT"
  BUILD_GO_BUILD_CACHE="${BUILD_CACHE_ROOT}/go-build"
  BUILD_HOST_GOOS="$("$go_bin" env GOOS)"
  BUILD_HOST_GOARCH="$("$go_bin" env GOARCH)"
  BUILD_SQLITE_INCLUDE_DIR="${BUILD_CACHE_ROOT}/sqlite/include"

  prepare_sqlite_headers "$go_bin"
  BUILD_GO_VERSION="$(builder_go_version)"
  [ -n "$BUILD_GO_VERSION" ] || die "failed to detect builder Go version"

  mkdir -p "$(dist_dir "$version")"

  IFS=',' read -r -a target_array <<<"$targets"
  if ! is_positive_int "$jobs"; then
    jobs="$(recommended_build_jobs "${#target_array[@]}")"
  fi
  if [ "$jobs" -gt "${#target_array[@]}" ]; then
    jobs="${#target_array[@]}"
  fi
  if [ "$jobs" -lt 1 ]; then
    jobs=1
  fi

  log "release build jobs: ${jobs}"
  log "release cache dir: ${BUILD_CACHE_ROOT}"
  build_started_at="$(epoch_now)"
  run_target_builds "$version" "$jobs" "${target_array[@]}"

  write_manifest \
    "$version" \
    "$targets" \
    "$BUILD_BUILDER_IMAGE" \
    "$BUILD_GO_VERSION"
  write_checksums "$version"
  build_elapsed=$(( $(epoch_now) - build_started_at ))
  log "build stage finished in $(format_duration_seconds "$build_elapsed")"
}

mirror_api_base() {
  local mirror_host="$1"

  printf '%s/mirrors/api' "$(trim_trailing_slash "$mirror_host")"
}

mirror_repo_base() {
  local mirror_host="$1"
  local repo_name="$2"

  printf '%s/repository/generic/%s' \
    "$(trim_trailing_slash "$mirror_host")" \
    "$repo_name"
}

mirror_prefix_base() {
  local mirror_host="$1"
  local repo_name="$2"
  local path_prefix="$3"

  printf '%s/%s' \
    "$(mirror_repo_base "$mirror_host" "$repo_name")" \
    "${path_prefix#/}"
}

mirror_release_base() {
  local mirror_host="$1"
  local repo_name="$2"
  local path_prefix="$3"
  local version="$4"

  printf '%s/%s/%s' \
    "$(mirror_prefix_base "$mirror_host" "$repo_name" "$path_prefix")" \
    "$releasesDirName" \
    "$version"
}

mirror_latest_base() {
  local mirror_host="$1"
  local repo_name="$2"
  local path_prefix="$3"

  printf '%s/%s' \
    "$(mirror_prefix_base "$mirror_host" "$repo_name" "$path_prefix")" \
    "$latestDirName"
}

mirror_channel_base() {
  local mirror_host="$1"
  local repo_name="$2"
  local path_prefix="$3"
  local channel="$4"

  printf '%s/%s' \
    "$(mirror_prefix_base "$mirror_host" "$repo_name" "$path_prefix")" \
    "$channel"
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

is_preview_version() {
  case "$1" in
    *-preview*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

release_channel_for_version() {
  if is_preview_version "$1"; then
    printf '%s' "$previewDirName"
    return
  fi
  printf '%s' "$latestDirName"
}

validate_channel_version() {
  local channel="$1"
  local version="$2"

  case "$channel" in
    "$latestDirName")
      if is_preview_version "$version"; then
        die "latest channel cannot publish preview version: ${version}"
      fi
      ;;
    "$previewDirName")
      if ! is_preview_version "$version"; then
        die "preview channel requires a preview version: ${version}"
      fi
      ;;
  esac
}

mirror_auth_value() {
  local username_env="$1"
  local token_env="$2"
  local username token

  username="$(printenv "$username_env" || true)"
  token="$(printenv "$token_env" || true)"
  [ -n "$username" ] || die "missing env: ${username_env}"
  [ -n "$token" ] || die "missing env: ${token_env}"
  printf '%s:%s' "$username" "$token"
}

verify_mirror_repo_access() {
  local mirror_host="$1"
  local repo_name="$2"
  local auth="$3"
  local response

  response="$(curl -fsS -u "$auth" \
    "$(mirror_api_base "$mirror_host")/generic/repo/info?repo_name=${repo_name}")"
  if ! printf '%s' "$response" | \
    grep -Eq '"code"[[:space:]]*:[[:space:]]*0'; then
    die "repo check failed for ${repo_name}: ${response}"
  fi
  if ! printf '%s' "$response" | \
    grep -Eq '"current_user_permission"[[:space:]]*:[[:space:]]*"admin"'; then
    die "repo check failed for ${repo_name}: ${response}"
  fi
}

upload_file() {
  local auth="$1"
  local source_path="$2"
  local target_url="$3"

  log "upload $(basename "$source_path")"
  curl -fsS --retry 3 -u "$auth" \
    -T "$source_path" "$target_url" >/dev/null
}

write_channel_default_script() {
  local script_name="$1"
  local channel="$2"
  local output_path="$3"

  python3 - \
    "$channel" \
    "$(module_dir)/${script_name}" \
    "$output_path" <<'PY'
import pathlib
import sys

channel = sys.argv[1]
source_path = pathlib.Path(sys.argv[2])
output_path = pathlib.Path(sys.argv[3])

channel_vars = {
    "latest": "latestDirName",
    "preview": "previewDirName",
}
try:
    channel_var = channel_vars[channel]
except KeyError:
    raise SystemExit(f"unsupported release channel: {channel}")

source = source_path.read_text()
needle = 'readonly defaultReleaseChannel="$latestDirName"'
replacement = f'readonly defaultReleaseChannel="${channel_var}"'
if needle not in source:
    raise SystemExit("default release channel declaration not found")

output_path.write_text(source.replace(needle, replacement, 1))
PY
}

download_optional_file() {
  local auth="$1"
  local url="$2"
  local output="$3"

  if curl -fsS -u "$auth" "$url" -o "$output" >/dev/null 2>&1; then
    return 0
  fi
  rm -f "$output"
  return 1
}

generate_release_index() {
  local version="$1"
  local channel="$2"
  local version_base="$3"
  local existing_path="$4"
  local output_path="$5"

  python3 - \
    "$version" \
    "$channel" \
    "$version_base" \
    "$existing_path" \
    "$output_path" \
    "$(module_dir)/${CHANGELOG_DOC_NAME}" \
    "$minRuntimeTargetVersion" <<'PY'
import datetime
import json
import pathlib
import re
import sys

version = sys.argv[1].strip()
channel = sys.argv[2].strip()
version_base = sys.argv[3].strip()
existing_path = pathlib.Path(sys.argv[4].strip())
output_path = pathlib.Path(sys.argv[5].strip())
changelog_path = pathlib.Path(sys.argv[6].strip())
min_target = sys.argv[7].strip()

def version_key(raw):
    raw = str(raw).strip().lstrip("vV")
    parts = []
    for chunk in raw.split("."):
        if not chunk:
            parts.append(0)
            continue
        match = re.match(r"(\d+)", chunk)
        if match:
            parts.append(int(match.group(1)))
        else:
            parts.append(-1)
    return tuple(parts)

def release_heading_version(line):
    pattern = r"^##\s+(v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)\b"
    match = re.match(pattern, line.strip())
    if match:
        return match.group(1)
    return ""

release_notes = {}
release_versions = []
current_version = ""
current_notes = []
current_item = ""
for raw_line in changelog_path.read_text(encoding="utf-8").splitlines():
    line = raw_line.strip()
    heading_version = release_heading_version(line)
    if heading_version:
        if current_version:
            if current_item.strip():
                current_notes.append(current_item.strip())
            release_notes[current_version] = current_notes[:5]
        current_version = heading_version
        release_versions.append(current_version)
        current_notes = []
        current_item = ""
        continue
    if not current_version:
        continue
    if line.startswith("- "):
        if current_item.strip():
            current_notes.append(current_item.strip())
        current_item = line[2:].strip()
    elif line == "":
        if current_item.strip():
            current_notes.append(current_item.strip())
            current_item = ""
    elif current_item:
        current_item += " " + line
if current_version:
    if current_item.strip():
        current_notes.append(current_item.strip())
    release_notes[current_version] = current_notes[:5]

notes = release_notes.get(version, [])
release_root = version_base.rsplit("/", 1)[0]

def version_urls(raw_version):
    release_base = f"{release_root}/{raw_version}"
    return {
        "install_url": f"{release_base}/install.sh",
        "start_script_url": f"{release_base}/start.sh",
        "changelog_url": f"{release_base}/CHANGELOG.md",
    }

min_target_key = version_key(min_target) if min_target else None

def version_supported(raw_version):
    if min_target_key is None:
        return True
    return version_key(raw_version) >= min_target_key

def is_preview_version(raw_version):
    return "-preview" in str(raw_version).strip()

def version_in_channel(raw_version):
    if channel == "preview":
        return is_preview_version(raw_version)
    return not is_preview_version(raw_version)

index = {
    "latest_version": version,
    "channel": channel,
    "min_supported_target": min_target,
    "versions": [],
}
if existing_path.exists():
    try:
        index = json.loads(existing_path.read_text(encoding="utf-8"))
    except Exception:
        index = {
            "latest_version": version,
            "channel": channel,
            "min_supported_target": min_target,
            "versions": [],
        }

entry = {
    "version": version,
    "published_at": datetime.datetime.now(
        datetime.timezone.utc,
    ).replace(microsecond=0).isoformat().replace("+00:00", "Z"),
    **version_urls(version),
    "notes": notes,
}

versions = []
seen = {version}
versions.append(entry)
for candidate in index.get("versions", []):
    current_version = str(candidate.get("version", "")).strip()
    if not current_version or current_version in seen:
        continue
    if not version_in_channel(current_version):
        continue
    seen.add(current_version)
    normalized = dict(candidate)
    normalized.update(version_urls(current_version))
    normalized["version"] = current_version
    normalized["notes"] = release_notes.get(
        current_version,
        candidate.get("notes", []),
    )[:5]
    versions.append(normalized)

for historical_version in release_versions:
    if not historical_version or historical_version in seen:
        continue
    if not version_in_channel(historical_version):
        continue
    if not version_supported(historical_version):
        continue
    seen.add(historical_version)
    versions.append({
        "version": historical_version,
        **version_urls(historical_version),
        "notes": release_notes.get(historical_version, []),
    })

versions.sort(
    key=lambda item: version_key(item.get("version", "")),
    reverse=True,
)

index["latest_version"] = version
index["channel"] = channel
index["min_supported_target"] = min_target
index["versions"] = versions
output_path.write_text(
    json.dumps(index, ensure_ascii=False, indent=2) + "\n",
    encoding="utf-8",
)
PY
}

publish_mirror() {
  local version="$1"
  local mirror_host="$2"
  local repo_name="$3"
  local path_prefix="$4"
  local channel="$5"
  local username_env="$6"
  local token_env="$7"
  local auth dist version_base channel_base prefix_base
  local version_file archive existing_index release_index
  local install_script start_script

  [ -d "$(dist_dir "$version")" ] || \
    die "missing dist/${version}; run build first"

  require_cmd curl
  require_cmd python3

  channel="$(normalize_release_channel "$channel")"
  validate_channel_version "$channel" "$version"

  auth="$(mirror_auth_value "$username_env" "$token_env")"
  verify_mirror_repo_access "$mirror_host" "$repo_name" "$auth"

  dist="$(dist_dir "$version")"
  prefix_base="$(mirror_prefix_base "$mirror_host" "$repo_name" "$path_prefix")"
  version_base="$(mirror_release_base \
    "$mirror_host" "$repo_name" "$path_prefix" "$version")"
  channel_base="$(mirror_channel_base \
    "$mirror_host" "$repo_name" "$path_prefix" "$channel")"

  version_file="$(mktemp)"
  existing_index="$(mktemp)"
  release_index="$(mktemp)"
  install_script="$(mktemp)"
  start_script="$(mktemp)"
  printf '%s\n' "$version" >"$version_file"
  write_channel_default_script \
    "$installScriptName" \
    "$channel" \
    "$install_script"
  write_channel_default_script \
    "$startScriptName" \
    "$channel" \
    "$start_script"
  download_optional_file \
    "$auth" \
    "${channel_base}/${releaseIndexFileName}" \
    "$existing_index" || true
  generate_release_index \
    "$version" \
    "$channel" \
    "$version_base" \
    "$existing_index" \
    "$release_index"

  upload_file \
    "$auth" \
    "$install_script" \
    "${version_base}/${installScriptName}"
  upload_file \
    "$auth" \
    "$install_script" \
    "${channel_base}/${installScriptName}"
  upload_file \
    "$auth" \
    "$start_script" \
    "${version_base}/${startScriptName}"
  upload_file \
    "$auth" \
    "$start_script" \
    "${channel_base}/${startScriptName}"
  upload_file \
    "$auth" \
    "$version_file" \
    "${channel_base}/${versionFileName}"
  if [ "$channel" = "$latestDirName" ]; then
    upload_file \
      "$auth" \
      "$version_file" \
      "${prefix_base}/${versionFileName}"
  fi
  upload_file \
    "$auth" \
    "$(module_dir)/${INSTALL_DOC_NAME}" \
    "${channel_base}/${INSTALL_DOC_NAME}"
  upload_file \
    "$auth" \
    "$(module_dir)/${INSTALL_DOC_NAME}" \
    "${version_base}/${INSTALL_DOC_NAME}"
  upload_file \
    "$auth" \
    "$(module_dir)/${CHANGELOG_DOC_NAME}" \
    "${channel_base}/${CHANGELOG_DOC_NAME}"
  upload_file \
    "$auth" \
    "$(module_dir)/${CHANGELOG_DOC_NAME}" \
    "${version_base}/${CHANGELOG_DOC_NAME}"
  upload_file \
    "$auth" \
    "${dist}/${manifestFileName}" \
    "${version_base}/${manifestFileName}"
  upload_file \
    "$auth" \
    "${dist}/${manifestFileName}" \
    "${channel_base}/${manifestFileName}"
  upload_file \
    "$auth" \
    "$release_index" \
    "${channel_base}/${releaseIndexFileName}"
  upload_file \
    "$auth" \
    "$release_index" \
    "${version_base}/${releaseIndexFileName}"
  upload_file \
    "$auth" \
    "${dist}/${checksumsFileName}" \
    "${version_base}/${checksumsFileName}"

  for archive in "${dist}"/*.tar.gz; do
    upload_file "$auth" "$archive" \
      "${version_base}/$(basename "$archive")"
  done

  rm -f "$version_file"
  rm -f "$existing_index"
  rm -f "$release_index"

  log ""
  log "Mirror publish complete."
  log "Install URL:"
  log "  ${channel_base}/${installScriptName}"
  log "Channel version URL:"
  log "  ${channel_base}/${versionFileName}"
}

main() {
  local command="${1:-}"
  local version=""
  local targets="$DEFAULT_TARGETS"
  local builder_image="$DEFAULT_BUILDER_IMAGE"
  local build_jobs="${OPENCLAW_RELEASE_JOBS:-}"
  local cache_dir="${OPENCLAW_RELEASE_CACHE_DIR:-$(release_cache_dir)}"
  local mirror_host="$DEFAULT_MIRROR_HOST"
  local repo_name="$DEFAULT_MIRROR_REPO_NAME"
  local path_prefix="$DEFAULT_MIRROR_PATH_PREFIX"
  local channel="$defaultReleaseChannel"
  local username_env="$DEFAULT_USERNAME_ENV"
  local token_env="$DEFAULT_TOKEN_ENV"

  if [ "$#" -eq 0 ]; then
    usage
    exit 1
  fi
  shift

  while [ "$#" -gt 0 ]; do
    case "$1" in
      --version)
        version="${2:-}"
        shift 2
        ;;
      --targets)
        targets="${2:-}"
        shift 2
        ;;
      --builder-image)
        builder_image="${2:-}"
        shift 2
        ;;
      --jobs)
        build_jobs="${2:-}"
        shift 2
        ;;
      --cache-dir)
        cache_dir="${2:-}"
        shift 2
        ;;
      --mirror-host)
        mirror_host="${2:-}"
        shift 2
        ;;
      --repo-name)
        repo_name="${2:-}"
        shift 2
        ;;
      --path-prefix)
        path_prefix="${2:-}"
        shift 2
        ;;
      --channel)
        channel="${2:-}"
        shift 2
        ;;
      --username-env)
        username_env="${2:-}"
        shift 2
        ;;
      --token-env)
        token_env="${2:-}"
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

  case "$command" in
    build)
      build_release \
        "$version" \
        "$targets" \
        "$builder_image" \
        "$build_jobs" \
        "$cache_dir"
      ;;
    publish)
      publish_mirror \
        "$version" \
        "$mirror_host" \
        "$repo_name" \
        "$path_prefix" \
        "$channel" \
        "$username_env" \
        "$token_env"
      ;;
    release)
      build_release \
        "$version" \
        "$targets" \
        "$builder_image" \
        "$build_jobs" \
        "$cache_dir"
      publish_mirror \
        "$version" \
        "$mirror_host" \
        "$repo_name" \
        "$path_prefix" \
        "$channel" \
        "$username_env" \
        "$token_env"
      ;;
    publish-git)
      die "publish-git was removed; use publish"
      ;;
    *)
      usage
      exit 1
      ;;
  esac
}

main "$@"
