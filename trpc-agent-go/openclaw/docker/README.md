# trpc-claw local Linux image

This directory contains a reproducible local image build for
`trpc-claw` on Linux.

The image intentionally does two separate things:

- It keeps a stable Linux baseline on top of
  `mirrors.tencent.com/todacc/trpc-golang-compile:0.3.2`.
- It reuses `trpc-claw bootstrap deps --bundled -json` as the primary
  dependency source, executes that plan inside the image, and then
  fills the remaining Linux-only gaps from a structured manifest
  instead of scattering one-off shell logic.

## Build

Build from the repository root.
`openclaw/go.mod` uses `replace ../`, so using `openclaw/` as the build
context will miss the parent module.

```bash
docker build -f openclaw/Dockerfile -t trpc-claw:local .
```

Or use the bundled helper, which always rebuilds from the current
workspace and then replaces one local container:

```bash
./openclaw/docker/local_up.sh --mode mock
```

By default the helper:

- builds `trpc-claw:local` from the current repository checkout
- removes and recreates `trpc-claw-local`
- publishes the gateway on `127.0.0.1:8080`
- publishes the admin UI on `127.0.0.1:19789`
- auto-picks the next nearby host port if those default host ports are
  already occupied
- mounts the repository root at `/workspace`
- rewrites local loopback endpoints such as
  `LANGFUSE_HOST=127.0.0.1:3000` to
  `host.docker.internal:3000` for the container side
- keeps `LANGFUSE_UI_BASE_URL` browser-facing. When the helper
  auto-detects a host-side local Langfuse, it sets
  `LANGFUSE_HOST=host.docker.internal:3000` for the container and
  `LANGFUSE_UI_BASE_URL=http://127.0.0.1:3000` for admin links.
- keeps admin reachable on the host browser with
  `http://127.0.0.1:19789/`

Useful build args:

- `BASE_IMAGE`: override the compile base image.
- `NODE_DIST_VERSION`: override the pinned Node.js tarball version.
- `TRPC_CLAW_STATE_DIR`: override the default in-image state directory.

Example:

```bash
docker build \
  -f openclaw/Dockerfile \
  -t trpc-claw:local \
  --build-arg NODE_DIST_VERSION=v24.14.0 \
  .
```

If you only want the Linux runtime environment and toolchains, without
copying repository files or baking in the `trpc-claw` binary, use the
standalone environment image:

```bash
mkdir -p /tmp/openclaw-empty-context
docker build \
  -f "$(pwd)/openclaw/Dockerfile.env" \
  -t trpc-claw-env:local \
  /tmp/openclaw-empty-context
```

`openclaw/Dockerfile.env` does not `COPY` from the build context, so it
can be built from an empty directory or any other context.

## What the image contains

The image bakes in:

- `trpc-claw` built from the current workspace.
- Default config files under `${TRPC_CLAW_STATE_DIR}`
  (default `/home/openclaw/.trpc-agent-go/openclaw`).
- Bundled skills under
  `${TRPC_CLAW_STATE_DIR}/skills/bundled`.
- A managed toolchain under
  `${TRPC_CLAW_STATE_DIR}/toolchain/python`.
- Browser runtime for Playwright.
- Linux baseline packages for office, OCR, browser automation, media,
  tmux, jq, ripgrep, and Chinese/emoji font coverage.
- Linux replacements for bundled-skill gaps such as `gh`, `codex`,
  `gemini`, `uv`, `openhue`, `obsidian-cli`, `whisper`, `himalaya`,
  `spogo`, `ffmpeg`, `ffprobe`, and other CLI tools that upstream
  bootstrap metadata does not fully cover on Linux yet.

The environment-only image keeps the same
`TRPC_CLAW_STATE_DIR`/toolchain layout and preinstalls the same general
Linux tool families, but it intentionally does not include:

- `trpc-claw`
- bundled skills
- repo-baked config files
- repo-driven `bootstrap deps --bundled` validation

During the build, the installer re-runs:

```bash
trpc-claw inspect deps --state-dir /home/openclaw/.trpc-agent-go/openclaw \
  --bundled -json
```

and fails the image build if bundled skill binaries or Python packages
are still unresolved.

## Run

Start an interactive shell with the default workspace mounted:

```bash
docker run --rm -it \
  -v "$(pwd)":/workspace \
  trpc-claw:local \
  bash
```

Start the long-running local service with the helper:

```bash
./openclaw/docker/local_up.sh --attach
```

Use `--env-file` if you want to keep runtime credentials out of the
shell:

```bash
./openclaw/docker/local_up.sh \
  --env-file .env.openclaw \
  --attach
```

If the container should be able to push back to `git.woa.com` over
HTTPS, pass a token at runtime. The image entrypoint enables git
credential storage automatically and seeds it from the first non-empty
token in:

- `GIT_CREDENTIAL_TOKEN`
- `GITLAB_TOKEN`
- `GIT_TOKEN`
- `OAUTH_TOKEN`

You can override the default HTTPS credential target with:

- `GIT_CREDENTIAL_HOST` (default `git.woa.com`)
- `GIT_CREDENTIAL_PROTOCOL` (default `https`)
- `GIT_CREDENTIAL_USERNAME` (default `oauth2`)

If you want to reuse host configs instead of the image-baked defaults:

```bash
./openclaw/docker/local_up.sh \
  --config ~/.trpc-agent-go/openclaw/openclaw.yaml \
  --trpc-config ~/.trpc-agent-go/openclaw/trpc_go.yaml
```

Run a quick health check:

```bash
docker run --rm -it \
  -v "$(pwd)":/workspace \
  trpc-claw:local \
  trpc-claw doctor
```

Inspect bundled deps inside the built image:

```bash
docker run --rm trpc-claw:local \
  trpc-claw inspect deps --bundled
```

## Notes

- The image includes installable tooling, not live credentials.
  Environment variables such as `OPENAI_API_KEY`,
  `WECOM_STREAM_BOT_ID`, `WECOM_STREAM_SECRET`,
  `LANGFUSE_PUBLIC_KEY`, and `LANGFUSE_SECRET_KEY`
  still need to be provided at runtime.
- For Langfuse, `LANGFUSE_HOST`, `LANGFUSE_PUBLIC_KEY`, and
  `LANGFUSE_SECRET_KEY` are the minimal trace upload variables.
  `LANGFUSE_UI_BASE_URL` plus `LANGFUSE_INIT_PROJECT_ID`, or a full
  `LANGFUSE_TRACE_URL_TEMPLATE`, enables clickable admin trace links.
- The image now keeps its writable global git config at
  `${TRPC_CLAW_STATE_DIR}/gitconfig` and its credential store at
  `${TRPC_CLAW_STATE_DIR}/git-credentials`. On startup it also marks
  repositories as safe for git and, when `${HOME}/.gitconfig` is mounted
  in, includes that host config instead of overwriting it.
- The image does not rewrite `https://` remotes to `http://`.
  If your repo remote is still SSH-based, keep using SSH agent
  forwarding or switch that remote to HTTPS explicitly.
- Docker already reuses unchanged local build layers by default.
  When the Dockerfile and earlier copied inputs do not change, repeated
  `docker build` or `./openclaw/docker/local_up.sh` runs will reuse
  cache automatically. Use `--pull` only when you want to refresh base
  layers, and `--no-cache` only when you explicitly need a cold rebuild.
- The helper forces `trpc-claw --admin-addr 0.0.0.0:<port>` inside the
  container and maps that port back to `127.0.0.1:<port>` on the host.
  This avoids the common container trap where admin is left on
  container-local `127.0.0.1` and becomes unreachable from the host.
- The helper adds `--add-host host.docker.internal:host-gateway` so
  container-side clients can reach host-side services such as a local
  Langfuse instance.
- The helper uses `--shm-size=1g` and `--init` because browser-heavy
  skills and spawned subprocesses are more stable with a larger shared
  memory segment and a proper PID 1 reaper.
- `op` is installed from the pinned official 1Password CLI release
  archive instead of being copied from a separate Docker stage. This
  keeps the image compatible with build platforms that inject
  per-stage init commands.
- Node.js is installed from the official Node.js release tarball because
  the TencentOS `nodejs` package in the base image is too old for modern
  CLIs such as `codex` and `gemini`.
- `ffmpeg` and `ffprobe` are installed from a pinned static upstream
  bundle because the TencentOS repositories available in the base image
  do not provide the needed packages.
- `whisper` uses CPU-only PyTorch wheels to avoid pulling the full CUDA
  dependency stack into the image.
- Avoid `bash -lc '...'` when you are sanity-checking the image.
  A login shell can reset `PATH` and hide the preinstalled toolchain.
  Use direct commands or plain `bash` instead.
