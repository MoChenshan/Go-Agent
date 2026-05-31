#!/usr/bin/env bash
set -euo pipefail

readonly defaultImageTag="trpc-claw:local"
readonly defaultContainerName="trpc-claw-local"
readonly defaultBindHost="127.0.0.1"
readonly defaultGatewayPort="8080"
readonly defaultAdminPort="19789"
readonly defaultGatewayContainerPort="8080"
readonly defaultContainerHome="/home/openclaw"
readonly defaultStateDir="${defaultContainerHome}/.trpc-agent-go/openclaw"
readonly defaultConfigPathInImage="${defaultStateDir}/openclaw.yaml"
readonly defaultWaitTimeoutSeconds="60"
readonly defaultShmSize="1g"
readonly hostGatewayName="host.docker.internal"
readonly hostGatewayMapping="${hostGatewayName}:host-gateway"
readonly defaultTrpcClawDisableUpgradeCheck="1"

IMAGE_TAG="${defaultImageTag}"
CONTAINER_NAME="${defaultContainerName}"
BIND_HOST="${defaultBindHost}"
HOST_GATEWAY_PORT="${defaultGatewayPort}"
HOST_ADMIN_PORT="${defaultAdminPort}"
WAIT_TIMEOUT_SECONDS="${defaultWaitTimeoutSeconds}"
SHM_SIZE="${defaultShmSize}"
MODEL_MODE=""
MODEL_NAME=""
OPENCLAW_CONFIG_HOST_PATH=""
TRPC_CONFIG_HOST_PATH=""
ENV_FILE_PATH=""
ATTACH_LOGS="0"
PULL_BASE_IMAGE="0"
NO_BUILD_CACHE="0"
HOST_GATEWAY_PORT_EXPLICIT="0"
HOST_ADMIN_PORT_EXPLICIT="0"

declare -a BUILD_ARGS=()
declare -a EXTRA_TRPC_ARGS=()
declare -a dockerRunArgs=()
declare -a trpcArgs=()

usage() {
  cat <<'EOF'
Build the local trpc-claw image from the current workspace and start
one container from it.

Usage:
  openclaw/docker/local_up.sh [options] [-- <extra trpc-claw args>]

Options:
  --image <tag>            Image tag to build and run.
                           Default: trpc-claw:local
  --name <name>            Container name.
                           Default: trpc-claw-local
  --bind-host <host>       Host bind address for published ports.
                           Default: 127.0.0.1
  --http-port <port>       Host port mapped to container gateway 8080.
                           Default: 8080
  --admin-port <port>      Host and container admin port.
                           Default: 19789
  --config <path>          Host OpenClaw config file to mount read-only.
                           Default: use the image-baked config
  --trpc-config <path>     Host trpc_go.yaml to mount read-only.
                           Default: use the image-baked trpc_go.yaml
  --env-file <path>        Docker env-file for runtime credentials.
  --mode <mode>            trpc-claw -mode value, for example mock.
  --model <model>          trpc-claw -model value.
  --build-arg <key=value>  Extra docker build arg. Repeatable.
  --pull                   Pass --pull to docker build.
  --no-cache               Pass --no-cache to docker build.
  --attach                 Follow container logs after startup.
  -h, --help               Show this help.

Examples:
  openclaw/docker/local_up.sh --mode mock
  openclaw/docker/local_up.sh --attach --env-file .env.openclaw
  openclaw/docker/local_up.sh \
    --config ~/.trpc-agent-go/openclaw/openclaw.yaml \
    --trpc-config ~/.trpc-agent-go/openclaw/trpc_go.yaml
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
  git -C "$(dirname "${BASH_SOURCE[0]}")/.." \
    rev-parse --show-toplevel
}

module_dir() {
  printf '%s/openclaw' "$(repo_root)"
}

validate_file() {
  local path="$1"
  if [ ! -f "$path" ]; then
    die "file not found: $path"
  fi
}

append_optional_ro_mount() {
  local hostPath="$1"
  local containerPath="$2"

  if [ -e "$hostPath" ]; then
    dockerRunArgs+=(
      --volume
      "${hostPath}:${containerPath}:ro"
    )
  fi
}

append_passthrough_env() {
  local name="$1"

  if [ -n "${!name-}" ]; then
    dockerRunArgs+=(--env "$name")
  fi
}

append_env_value() {
  local name="$1"
  local value="$2"

  dockerRunArgs+=(--env "${name}=${value}")
}

rewrite_local_endpoint() {
  local value="$1"

  case "$value" in
    '')
      printf '%s' ""
      ;;
    localhost)
      printf '%s' "${hostGatewayName}"
      ;;
    127.0.0.1)
      printf '%s' "${hostGatewayName}"
      ;;
    '[::1]')
      printf '%s' "${hostGatewayName}"
      ;;
    localhost:*)
      printf '%s:%s' "${hostGatewayName}" "${value#localhost:}"
      ;;
    127.0.0.1:*)
      printf '%s:%s' "${hostGatewayName}" "${value#127.0.0.1:}"
      ;;
    '[::1]':*)
      printf '%s:%s' "${hostGatewayName}" "${value#\[::1\]:}"
      ;;
    http://localhost)
      printf 'http://%s' "${hostGatewayName}"
      ;;
    https://localhost)
      printf 'https://%s' "${hostGatewayName}"
      ;;
    ws://localhost)
      printf 'ws://%s' "${hostGatewayName}"
      ;;
    wss://localhost)
      printf 'wss://%s' "${hostGatewayName}"
      ;;
    http://127.0.0.1)
      printf 'http://%s' "${hostGatewayName}"
      ;;
    https://127.0.0.1)
      printf 'https://%s' "${hostGatewayName}"
      ;;
    ws://127.0.0.1)
      printf 'ws://%s' "${hostGatewayName}"
      ;;
    wss://127.0.0.1)
      printf 'wss://%s' "${hostGatewayName}"
      ;;
    http://[::1])
      printf 'http://%s' "${hostGatewayName}"
      ;;
    https://[::1])
      printf 'https://%s' "${hostGatewayName}"
      ;;
    ws://[::1])
      printf 'ws://%s' "${hostGatewayName}"
      ;;
    wss://[::1])
      printf 'wss://%s' "${hostGatewayName}"
      ;;
    http://localhost/*)
      printf 'http://%s/%s' \
        "${hostGatewayName}" \
        "${value#http://localhost/}"
      ;;
    https://localhost/*)
      printf 'https://%s/%s' \
        "${hostGatewayName}" \
        "${value#https://localhost/}"
      ;;
    ws://localhost/*)
      printf 'ws://%s/%s' \
        "${hostGatewayName}" \
        "${value#ws://localhost/}"
      ;;
    wss://localhost/*)
      printf 'wss://%s/%s' \
        "${hostGatewayName}" \
        "${value#wss://localhost/}"
      ;;
    http://127.0.0.1/*)
      printf 'http://%s/%s' \
        "${hostGatewayName}" \
        "${value#http://127.0.0.1/}"
      ;;
    https://127.0.0.1/*)
      printf 'https://%s/%s' \
        "${hostGatewayName}" \
        "${value#https://127.0.0.1/}"
      ;;
    ws://127.0.0.1/*)
      printf 'ws://%s/%s' \
        "${hostGatewayName}" \
        "${value#ws://127.0.0.1/}"
      ;;
    wss://127.0.0.1/*)
      printf 'wss://%s/%s' \
        "${hostGatewayName}" \
        "${value#wss://127.0.0.1/}"
      ;;
    http://[::1]/*)
      printf 'http://%s/%s' \
        "${hostGatewayName}" \
        "${value#http://\[::1\]/}"
      ;;
    https://[::1]/*)
      printf 'https://%s/%s' \
        "${hostGatewayName}" \
        "${value#https://\[::1\]/}"
      ;;
    ws://[::1]/*)
      printf 'ws://%s/%s' \
        "${hostGatewayName}" \
        "${value#ws://\[::1\]/}"
      ;;
    wss://[::1]/*)
      printf 'wss://%s/%s' \
        "${hostGatewayName}" \
        "${value#wss://\[::1\]/}"
      ;;
    http://localhost:*)
      printf 'http://%s:%s' \
        "${hostGatewayName}" \
        "${value#http://localhost:}"
      ;;
    https://localhost:*)
      printf 'https://%s:%s' \
        "${hostGatewayName}" \
        "${value#https://localhost:}"
      ;;
    ws://localhost:*)
      printf 'ws://%s:%s' \
        "${hostGatewayName}" \
        "${value#ws://localhost:}"
      ;;
    wss://localhost:*)
      printf 'wss://%s:%s' \
        "${hostGatewayName}" \
        "${value#wss://localhost:}"
      ;;
    http://127.0.0.1:*)
      printf 'http://%s:%s' \
        "${hostGatewayName}" \
        "${value#http://127.0.0.1:}"
      ;;
    https://127.0.0.1:*)
      printf 'https://%s:%s' \
        "${hostGatewayName}" \
        "${value#https://127.0.0.1:}"
      ;;
    ws://127.0.0.1:*)
      printf 'ws://%s:%s' \
        "${hostGatewayName}" \
        "${value#ws://127.0.0.1:}"
      ;;
    wss://127.0.0.1:*)
      printf 'wss://%s:%s' \
        "${hostGatewayName}" \
        "${value#wss://127.0.0.1:}"
      ;;
    http://[::1]:*)
      printf 'http://%s:%s' \
        "${hostGatewayName}" \
        "${value#http://\[::1\]:}"
      ;;
    https://[::1]:*)
      printf 'https://%s:%s' \
        "${hostGatewayName}" \
        "${value#https://\[::1\]:}"
      ;;
    ws://[::1]:*)
      printf 'ws://%s:%s' \
        "${hostGatewayName}" \
        "${value#ws://\[::1\]:}"
      ;;
    wss://[::1]:*)
      printf 'wss://%s:%s' \
        "${hostGatewayName}" \
        "${value#wss://\[::1\]:}"
      ;;
    *)
      printf '%s' "$value"
      ;;
  esac
}

host_has_langfuse() {
  curl -fsS --max-time 1 \
    http://127.0.0.1:3000 >/dev/null 2>&1
}

langfuse_ui_base_from_host() {
  local host="$1"
  local insecure="${2:-}"

  host="${host%/}"
  case "$host" in
    "")
      return 0
      ;;
    http://* | https://*)
      printf '%s' "$host"
      ;;
    localhost* | 127.0.0.1*)
      printf 'http://%s' "$host"
      ;;
    *)
      if [ "$insecure" = "true" ]; then
        printf 'http://%s' "$host"
      else
        printf 'https://%s' "$host"
      fi
      ;;
  esac
}

host_port_available() {
  local host="$1"
  local port="$2"

  python3 - "$host" "$port" <<'PY'
import socket
import sys

host = sys.argv[1]
port = int(sys.argv[2])

if host in ("", "0.0.0.0", "127.0.0.1", "localhost"):
    family = socket.AF_INET
elif host == "::":
    family = socket.AF_INET6
else:
    family = socket.AF_INET6 if ":" in host else socket.AF_INET

bind_host = host
if bind_host in ("", "localhost"):
    bind_host = "127.0.0.1"

sock = socket.socket(family, socket.SOCK_STREAM)
sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
try:
    sock.bind((bind_host, port))
except OSError:
    sys.exit(1)
finally:
    sock.close()

sys.exit(0)
PY
}

find_available_host_port() {
  local host="$1"
  local preferredPort="$2"
  local label="$3"
  local port="$preferredPort"
  local maxPort=$((preferredPort + 32))

  while [ "$port" -le "$maxPort" ]; do
    if host_port_available "$host" "$port"; then
      if [ "$port" != "$preferredPort" ]; then
        printf '%s\n' \
          "${label} port ${preferredPort} is busy on ${host}; using ${port}" \
          >&2
      fi
      printf '%s' "$port"
      return 0
    fi
    port=$((port + 1))
  done

  die "no free ${label} port found on ${host} from ${preferredPort} to ${maxPort}"
}

container_exists() {
  docker inspect "$1" >/dev/null 2>&1
}

container_running() {
  local running

  running="$(
    docker inspect -f '{{.State.Running}}' "$1" 2>/dev/null || true
  )"
  [ "$running" = "true" ]
}

wait_for_url() {
  local url="$1"
  local label="$2"
  local waited="0"

  while true; do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    if ! container_running "$CONTAINER_NAME"; then
      docker logs --tail 200 "$CONTAINER_NAME" >&2 || true
      die "container exited before ${label} became ready"
    fi
    if [ "$waited" -ge "$WAIT_TIMEOUT_SECONDS" ]; then
      docker logs --tail 200 "$CONTAINER_NAME" >&2 || true
      die "timed out waiting for ${label} at ${url}"
    fi
    sleep 1
    waited=$((waited + 1))
  done
}

probe_host() {
  case "$BIND_HOST" in
    0.0.0.0|'::'|'')
      printf '127.0.0.1'
      ;;
    *)
      printf '%s' "$BIND_HOST"
      ;;
  esac
}

parse_args() {
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --image)
        [ "$#" -ge 2 ] || die "flag $1 requires a value"
        IMAGE_TAG="$2"
        shift 2
        ;;
      --name)
        [ "$#" -ge 2 ] || die "flag $1 requires a value"
        CONTAINER_NAME="$2"
        shift 2
        ;;
      --bind-host)
        [ "$#" -ge 2 ] || die "flag $1 requires a value"
        BIND_HOST="$2"
        shift 2
        ;;
      --http-port)
        [ "$#" -ge 2 ] || die "flag $1 requires a value"
        HOST_GATEWAY_PORT="$2"
        HOST_GATEWAY_PORT_EXPLICIT="1"
        shift 2
        ;;
      --admin-port)
        [ "$#" -ge 2 ] || die "flag $1 requires a value"
        HOST_ADMIN_PORT="$2"
        HOST_ADMIN_PORT_EXPLICIT="1"
        shift 2
        ;;
      --config)
        [ "$#" -ge 2 ] || die "flag $1 requires a value"
        OPENCLAW_CONFIG_HOST_PATH="$2"
        shift 2
        ;;
      --trpc-config)
        [ "$#" -ge 2 ] || die "flag $1 requires a value"
        TRPC_CONFIG_HOST_PATH="$2"
        shift 2
        ;;
      --env-file)
        [ "$#" -ge 2 ] || die "flag $1 requires a value"
        ENV_FILE_PATH="$2"
        shift 2
        ;;
      --mode)
        [ "$#" -ge 2 ] || die "flag $1 requires a value"
        MODEL_MODE="$2"
        shift 2
        ;;
      --model)
        [ "$#" -ge 2 ] || die "flag $1 requires a value"
        MODEL_NAME="$2"
        shift 2
        ;;
      --build-arg)
        [ "$#" -ge 2 ] || die "flag $1 requires a value"
        BUILD_ARGS+=(--build-arg "$2")
        shift 2
        ;;
      --pull)
        PULL_BASE_IMAGE="1"
        shift
        ;;
      --no-cache)
        NO_BUILD_CACHE="1"
        shift
        ;;
      --attach)
        ATTACH_LOGS="1"
        shift
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      --)
        shift
        EXTRA_TRPC_ARGS=("$@")
        break
        ;;
      *)
        die "unknown argument: $1"
        ;;
    esac
  done
}

main() {
  local root
  local openclawConfigInContainer="${defaultConfigPathInImage}"
  local trpcConfigInContainer=""
  local langfuseHost=""
  local langfuseUIBaseURL=""
  local localLangfuseDetected="0"
  local hostProbe
  local imageID
  local resolvedGatewayHostPort="$HOST_GATEWAY_PORT"
  local resolvedAdminHostPort="$HOST_ADMIN_PORT"

  parse_args "$@"

  require_cmd docker
  require_cmd git
  require_cmd curl
  require_cmd python3

  root="$(repo_root)"

  if [ -n "$OPENCLAW_CONFIG_HOST_PATH" ]; then
    validate_file "$OPENCLAW_CONFIG_HOST_PATH"
    openclawConfigInContainer="/run/openclaw-config/$(basename \
      "$OPENCLAW_CONFIG_HOST_PATH")"
  fi
  if [ -n "$TRPC_CONFIG_HOST_PATH" ]; then
    validate_file "$TRPC_CONFIG_HOST_PATH"
    trpcConfigInContainer="/run/trpc-config/$(basename \
      "$TRPC_CONFIG_HOST_PATH")"
  fi
  if [ -n "$ENV_FILE_PATH" ]; then
    validate_file "$ENV_FILE_PATH"
  fi

  log "Building ${IMAGE_TAG} from $(module_dir)/Dockerfile"

  if [ "$PULL_BASE_IMAGE" = "1" ]; then
    BUILD_ARGS+=(--pull)
  fi
  if [ "$NO_BUILD_CACHE" = "1" ]; then
    BUILD_ARGS+=(--no-cache)
  fi

  DOCKER_BUILDKIT="${DOCKER_BUILDKIT:-1}" \
    docker build \
      -f "$(module_dir)/Dockerfile" \
      -t "$IMAGE_TAG" \
      "${BUILD_ARGS[@]}" \
      "$root"

  imageID="$(docker image inspect -f '{{.Id}}' "$IMAGE_TAG")"

  if container_exists "$CONTAINER_NAME"; then
    log "Removing existing container ${CONTAINER_NAME}"
    docker rm -f "$CONTAINER_NAME" >/dev/null
  fi

  if host_port_available "$BIND_HOST" "$HOST_GATEWAY_PORT"; then
    resolvedGatewayHostPort="$HOST_GATEWAY_PORT"
  elif [ "$HOST_GATEWAY_PORT_EXPLICIT" = "1" ]; then
    die "gateway host port ${HOST_GATEWAY_PORT} is busy on ${BIND_HOST}"
  else
    resolvedGatewayHostPort="$(
      find_available_host_port \
        "$BIND_HOST" \
        "$HOST_GATEWAY_PORT" \
        "gateway"
    )"
  fi

  if host_port_available "$BIND_HOST" "$HOST_ADMIN_PORT"; then
    resolvedAdminHostPort="$HOST_ADMIN_PORT"
  elif [ "$HOST_ADMIN_PORT_EXPLICIT" = "1" ]; then
    die "admin host port ${HOST_ADMIN_PORT} is busy on ${BIND_HOST}"
  else
    resolvedAdminHostPort="$(
      find_available_host_port \
        "$BIND_HOST" \
        "$HOST_ADMIN_PORT" \
        "admin"
    )"
  fi

  dockerRunArgs=(
    --detach
    --init
    --name "$CONTAINER_NAME"
    --workdir /workspace
    --shm-size "$SHM_SIZE"
    --add-host "$hostGatewayMapping"
    --publish
    "${BIND_HOST}:${resolvedGatewayHostPort}:${defaultGatewayContainerPort}"
    --publish
    "${BIND_HOST}:${resolvedAdminHostPort}:${HOST_ADMIN_PORT}"
    --volume
    "${root}:/workspace"
  )

  if [ -n "$OPENCLAW_CONFIG_HOST_PATH" ]; then
    dockerRunArgs+=(
      --volume
      "$(dirname "$OPENCLAW_CONFIG_HOST_PATH"):/run/openclaw-config:ro"
    )
  fi
  if [ -n "$TRPC_CONFIG_HOST_PATH" ]; then
    dockerRunArgs+=(
      --volume
      "$(dirname "$TRPC_CONFIG_HOST_PATH"):/run/trpc-config:ro"
    )
  fi
  if [ -n "$ENV_FILE_PATH" ]; then
    dockerRunArgs+=(--env-file "$ENV_FILE_PATH")
  fi

  append_optional_ro_mount \
    "${HOME}/.codex" \
    "${defaultContainerHome}/.codex"
  append_optional_ro_mount \
    "${HOME}/.gemini" \
    "${defaultContainerHome}/.gemini"
  append_optional_ro_mount \
    "${HOME}/.config/gh" \
    "${defaultContainerHome}/.config/gh"
  append_optional_ro_mount \
    "${HOME}/.config/op" \
    "${defaultContainerHome}/.config/op"
  append_optional_ro_mount \
    "${HOME}/.gitconfig" \
    "${defaultContainerHome}/.gitconfig"

  if [ -n "${SSH_AUTH_SOCK-}" ] && [ -S "${SSH_AUTH_SOCK}" ]; then
    dockerRunArgs+=(
      --volume
      "${SSH_AUTH_SOCK}:/run/host-ssh-agent"
      --env
      "SSH_AUTH_SOCK=/run/host-ssh-agent"
    )
  fi

  append_env_value \
    "TRPC_CLAW_DISABLE_UPGRADE_CHECK" \
    "${TRPC_CLAW_DISABLE_UPGRADE_CHECK:-${defaultTrpcClawDisableUpgradeCheck}}"

  append_passthrough_env HTTP_PROXY
  append_passthrough_env HTTPS_PROXY
  append_passthrough_env NO_PROXY
  append_passthrough_env OPENAI_API_KEY
  append_passthrough_env OPENAI_ORG_ID
  append_passthrough_env OPENAI_PROJECT_ID
  append_passthrough_env WECOM_STREAM_BOT_ID
  append_passthrough_env WECOM_STREAM_SECRET
  append_passthrough_env WECOM_BOT_NAME
  append_passthrough_env WECOM_CORP_ID
  append_passthrough_env WECOM_AGENT_ID
  append_passthrough_env WECOM_TOKEN
  append_passthrough_env WECOM_ENCODING_AES_KEY
  append_passthrough_env WECOM_AI_CALLBACK_PATH
  append_passthrough_env WECOM_NOTIFICATION_CALLBACK_PATH
  append_passthrough_env WECOM_WEBHOOK_URL
  append_passthrough_env LANGFUSE_ENABLED
  append_passthrough_env LANGFUSE_REQUIRED
  append_passthrough_env LANGFUSE_PUBLIC_KEY
  append_passthrough_env LANGFUSE_SECRET_KEY
  append_passthrough_env LANGFUSE_INIT_PROJECT_ID
  append_passthrough_env LANGFUSE_UI_BASE_URL
  append_passthrough_env LANGFUSE_TRACE_URL_TEMPLATE
  append_passthrough_env LANGFUSE_INSECURE
  append_passthrough_env LANGFUSE_OBSERVATION_LEAF_VALUE_MAX_BYTES
  append_passthrough_env GITHUB_TOKEN
  append_passthrough_env GH_TOKEN
  append_passthrough_env GITLAB_TOKEN
  append_passthrough_env GIT_CREDENTIAL_TOKEN
  append_passthrough_env GIT_CREDENTIAL_HOST
  append_passthrough_env GIT_CREDENTIAL_PROTOCOL
  append_passthrough_env GIT_CREDENTIAL_USERNAME
  append_passthrough_env GIT_TOKEN
  append_passthrough_env OAUTH_TOKEN
  append_passthrough_env ANTHROPIC_API_KEY
  append_passthrough_env GOOGLE_API_KEY
  append_passthrough_env GEMINI_API_KEY

  if [ -n "${OPENAI_BASE_URL-}" ]; then
    append_env_value \
      OPENAI_BASE_URL \
      "$(rewrite_local_endpoint "${OPENAI_BASE_URL}")"
  fi
  if [ -n "${WECOM_STREAM_WS_URL-}" ]; then
    append_env_value \
      WECOM_STREAM_WS_URL \
      "$(rewrite_local_endpoint "${WECOM_STREAM_WS_URL}")"
  fi

  if [ -n "${LANGFUSE_HOST-}" ]; then
    langfuseHost="$(rewrite_local_endpoint "${LANGFUSE_HOST}")"
    if [ "$langfuseHost" != "${LANGFUSE_HOST}" ]; then
      localLangfuseDetected="1"
      if [ -z "${LANGFUSE_UI_BASE_URL-}" ]; then
        langfuseUIBaseURL="$(
          langfuse_ui_base_from_host \
            "${LANGFUSE_HOST}" \
            "${LANGFUSE_INSECURE-}"
        )"
      fi
    fi
  elif host_has_langfuse; then
    langfuseHost="${hostGatewayName}:3000"
    langfuseUIBaseURL="http://127.0.0.1:3000"
    localLangfuseDetected="1"
  fi
  if [ -n "$langfuseHost" ]; then
    append_env_value LANGFUSE_HOST "$langfuseHost"
    if [ "$localLangfuseDetected" = "1" ] &&
      [ -z "${LANGFUSE_INSECURE-}" ]; then
      append_env_value LANGFUSE_INSECURE "true"
    fi
  fi
  if [ -n "$langfuseUIBaseURL" ]; then
    append_env_value LANGFUSE_UI_BASE_URL "$langfuseUIBaseURL"
  fi

  trpcArgs=(
    trpc-claw
    -config "$openclawConfigInContainer"
    -admin-addr "0.0.0.0:${HOST_ADMIN_PORT}"
    -admin-auto-port=false
  )

  if [ -n "$trpcConfigInContainer" ]; then
    trpcArgs+=(-conf "$trpcConfigInContainer")
  fi
  if [ -n "$MODEL_MODE" ]; then
    trpcArgs+=(-mode "$MODEL_MODE")
  fi
  if [ -n "$MODEL_NAME" ]; then
    trpcArgs+=(-model "$MODEL_NAME")
  fi
  if [ "${#EXTRA_TRPC_ARGS[@]}" -gt 0 ]; then
    trpcArgs+=("${EXTRA_TRPC_ARGS[@]}")
  fi

  log "Starting container ${CONTAINER_NAME}"

  docker run "${dockerRunArgs[@]}" "$IMAGE_TAG" "${trpcArgs[@]}" >/dev/null

  hostProbe="$(probe_host)"
  wait_for_url \
    "http://${hostProbe}:${resolvedGatewayHostPort}/healthz" \
    "gateway health endpoint"
  wait_for_url \
    "http://${hostProbe}:${resolvedAdminHostPort}/" \
    "admin UI"

  log "Image:     ${IMAGE_TAG}"
  log "Image ID:  ${imageID}"
  log "Container: ${CONTAINER_NAME}"
  log "Gateway:   http://${hostProbe}:${resolvedGatewayHostPort}/healthz"
  log "Admin UI:  http://${hostProbe}:${resolvedAdminHostPort}/"
  if [ -n "$langfuseHost" ]; then
    log "Langfuse:  container uses ${langfuseHost}"
  fi
  log "Logs:      docker logs -f ${CONTAINER_NAME}"
  log "Stop:      docker rm -f ${CONTAINER_NAME}"

  if [ "$ATTACH_LOGS" = "1" ]; then
    exec docker logs -f "$CONTAINER_NAME"
  fi
}

main "$@"
