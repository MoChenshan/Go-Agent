# trpc-claw 发布说明

本目录的发布链路有两类必须交付的结果：

- 腾讯云镜像 generic 仓库里的二进制包、安装脚本和 channel 指针。
- 仓库里指向同一个源码提交的 `openclaw/<version>` git tag。

发布不能只上传二进制。每次正式发版都必须同时推送对应 git tag，
让镜像里的 `SOURCE_COMMIT`、`CHANGELOG.md` 和仓库 tag 能互相对齐。
`trpc-claw` 是 `openclaw/` 子目录分发版，tag 必须使用
`openclaw/v0.0.46` 这种命名空间，不要占用根模块的 `v0.0.46`
tag。

二进制发布链路分为两部分：

- `release.sh build`
  负责构建预编译包。
- `release.sh publish`
  负责把安装脚本和构建产物上传到腾讯云镜像 generic 仓库。
  默认发布到 stable `latest/` channel；preview 版本必须显式加
  `--channel preview`。

也可以直接使用：

- `release.sh release`
  一次完成 build 和 publish。

## 设计原则

- 发布脚本不依赖 cookie。
- 发布脚本不把 token 写进仓库文件。
- 上传凭据只从环境变量里读取。
- 每个正式版本都必须有对应的 `openclaw/<version>` git tag，
  tag 指向用于构建发布包的同一个 commit。
- 用户安装侧不依赖工蜂登录态，也不依赖额外 token。
- 安装入口保持 `curl ... | bash`。
- 预编译包全部保留 CGO，确保 Linux / macOS 包都能直接使用
  `sqlite` / `sqlitevec` backend。

## 需要的权限

发布侧需要两类前提：

1. 你在腾讯云镜像 generic repo 上有写权限。
2. 你已经在镜像控制台拿到个人 token。

相关页面：

- Generic repo：
  `https://mirrors.tencent.com/#/private/generic2/detail?repo_name=trpc-agent-go&path=&search_value=trpc-agent-go&page_num=1`
- 我的权限：
  `https://mirrors.tencent.com/#/permission/my_permission`

脚本默认读取这两个环境变量：

- `TENCENT_USERNAME`
- `TENCENT_TOKEN`

可以放在你自己的 shell 配置里，例如 `~/.bashrc`，
但不要把值写进仓库脚本、文档示例或 CI 日志。

## 上传协议

发布脚本使用腾讯云镜像 generic 仓库的直传路径：

`https://mirrors.tencent.com/repository/generic/<repo>/<path>`

鉴权方式是 HTTP Basic Auth，用户名和 token 来自上面的环境变量。

当前默认上传到：

- repo：`trpc-agent-go`
- path prefix：`trpc-claw`

也就是：

`https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw`

## 目录布局

镜像里的目录会整理成：

```text
trpc-claw/
├── VERSION
├── latest/
│   ├── VERSION
│   ├── CHANGELOG.md
│   ├── INSTALL.md
│   ├── install.sh
│   ├── start.sh
│   ├── manifest.env
│   └── releases.json
├── preview/
│   ├── VERSION
│   ├── CHANGELOG.md
│   ├── INSTALL.md
│   ├── install.sh
│   ├── start.sh
│   ├── manifest.env
│   └── releases.json
└── releases/
    └── v0.0.46/
        ├── CHANGELOG.md
        ├── INSTALL.md
        ├── checksums.txt
        ├── install.sh
        ├── manifest.env
        ├── start.sh
        ├── trpc-claw-v0.0.46-linux-amd64.tar.gz
        ├── trpc-claw-v0.0.46-linux-arm64.tar.gz
        ├── trpc-claw-v0.0.46-darwin-amd64.tar.gz
        └── trpc-claw-v0.0.46-darwin-arm64.tar.gz
```

说明：

- `latest/` 永远指向当前推荐安装版本，也就是 stable channel。
- `preview/` 只用于显式 opt-in 的预览版本。
- 根 `VERSION` 只跟随 `latest/VERSION`，preview 发布不能改它。
- `latest/CHANGELOG.md` 供启动时版本提示读取最新 release 说明。
- `VERSION` 让安装脚本可以先查最新版本，再选择正确的包。
- `preview/CHANGELOG.md` 供显式 preview 安装和运行时升级查看说明。
- `releases/<version>/` 保存不可变的历史产物，stable 和 preview
  都放在这里。

## 本地构建

```bash
cd openclaw
bash ./release.sh build --version v0.0.46
```

发布包会同时带上：

- `start.sh` 平台入口样板
- `install.sh`，方便样板脚本直接复用同目录安装器
- `config/` 下的默认 profile
- `skills/` 下同步自 GitHub OpenClaw 的默认 skills

默认 target：

- `linux/amd64`
- `linux/arm64`
- `darwin/amd64`
- `darwin/arm64`

也可以自己指定：

```bash
bash ./release.sh build \
  --version v0.0.46 \
  --targets linux/amd64,darwin/arm64
```

也可以显式调并行度或 cache 目录：

```bash
bash ./release.sh build \
  --version v0.0.46 \
  --jobs 2 \
  --cache-dir ./dist/.release-cache
```

构建完成后，产物位于：

`openclaw/dist/v0.0.46/`

默认情况下：

- 构建并行度会按当前机器自动选择，
  但最大只会并行 2 个 target，
  避免 `xgo` 交叉编译在同一台发布机上过度抢占 CPU / IO
  后反而变慢。
- 构建 cache 会持久化到
  `openclaw/dist/.release-cache/`，
  让连续多次 release / rebuild 可以复用已有的 Go build cache。
- 如果你需要覆盖默认值，也可以直接用环境变量：
  `OPENCLAW_RELEASE_JOBS`、
  `OPENCLAW_RELEASE_CACHE_DIR`。

## 构建前提

`release.sh build` 依赖两样东西：

- 主机上的 Go SDK
  - 脚本会先用主机 Go 执行一次 `go mod download`，
    把依赖同步到主机 module cache。
  - 如果发布机 PATH 里的 `go` 不是期望版本，
    可以用 `OPENCLAW_GO_BIN` 指向主机上的 Go 可执行文件。
    这个 Go 只负责同步 module cache 和定位依赖，
    不会被直接挂进 Linux builder 里执行。
- 主机上的 Docker
  - 脚本会调用 `ghcr.io/crazy-max/xgo:latest`
    这个 builder 镜像，并在容器里对每个 target 执行
    带 CGO 的交叉编译。
  - 容器内默认使用 builder 镜像自带的
    `/usr/local/go`。如果将来更换 builder 镜像且 Go 安装目录不同，
    可以用 `OPENCLAW_BUILDER_GOROOT` 覆盖。
  - `sqlitevec` 需要的 `sqlite3.h` / `sqlite3ext.h`
    会在构建前自动从主机 Go module cache 里的
    `github.com/mattn/go-sqlite3` 提取并挂进容器，
    不需要再手工给发布机装额外的 SQLite 开发包。

构建过程中会额外做两层校验：

- 检查产物里是否真的带了 `sqlite3_open_v2` 等 sqlite 符号。
- 对“当前发布机同平台”的那一个包，直接跑
  `trpc-claw inspect plugins`，确认运行时里确实有
  `sqlite` / `sqlitevec` backend。

## 上传到腾讯云镜像

每次在这个 repo 里做 release，推荐按下面顺序操作：

1. 进入目录：

```bash
cd openclaw
```

2. 准备上传凭据：

```bash
export TENCENT_USERNAME='your_name'
export TENCENT_TOKEN='your_token'
```

3. 更新 [`CHANGELOG.md`](CHANGELOG.md)，
   补上这次版本对用户可见的主要变化，
   并把版本标题写成 `## v0.0.46 (2026-03-30)` 这种格式。
   这类 changelog 变更需要先提交、提 MR，并合入 `master`。

4. 切到合入后的最新 `master`，确认工作区干净：

```text
git checkout master
git pull --ff-only origin master
git status --short
```

5. 构建并上传：

```bash
bash ./release.sh release --version v0.0.46
```

6. 给本次发布对应的源码提交打 tag，并推送到主仓库：

```text
release_version="v0.0.46"
release_tag="openclaw/${release_version}"
release_commit="$(git rev-parse HEAD)"
git fetch origin --tags
if git rev-parse -q --verify "refs/tags/${release_tag}" >/dev/null; then
  echo "tag ${release_tag} already exists" >&2
  exit 1
fi
git tag -a "$release_tag" "$release_commit" \
  -m "trpc-claw $release_version"
git push origin "$release_tag"
```

注意：

- tag 必须指向刚刚执行 `release.sh release` 的同一个 commit。
- tag 名必须是 `openclaw/<version>`，例如 `openclaw/v0.0.46`。
  不要使用裸 `v0.0.46`，避免和根模块版本 tag 混在一起。
- 如果 tag 已经存在，先停下来确认镜像产物和 tag 指向是否一致，
  不要覆盖或复用错误 tag。
- 发版完成的判定是“镜像产物上传成功 + 对应 git tag 推送成功”，
  不能只完成其中一个。

7. 用用户视角做一次匿名安装 smoke test：

```bash
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
  | bash -s -- --version v0.0.46 --profile mock -f
```

确认：

- `~/.local/bin/trpc-claw` 存在

8. 验证 `latest/releases.json` 已更新到本次版本：

```bash
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/releases.json'
```

8.1. 如果这次有改平台入口样板，再确认镜像里的 `start.sh`
也已刷新：

```bash
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/start.sh'
```

8.2. 如果这次是 preview 发布，还要额外验证：

```bash
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/preview/VERSION'

curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/VERSION'
```

`preview/VERSION` 应该等于这次 preview 版本，`latest/VERSION`
必须仍然停留在当前正式版本。

9. 验证远程 git tag 已推送，并指向本次 release commit：

```text
git fetch origin --tags
git rev-list -n 1 openclaw/v0.0.46
git rev-parse HEAD
```

两个 commit 应该一致。

10. 如果这次 release 改了默认模板、`upgrade` 行为，
   再补一轮 CLI 视角 smoke test：

```bash
trpc-claw upgrade -f --profile wecom-ai-websocket
```

确认以下两点：

- `~/.trpc-agent-go/openclaw/openclaw.yaml`
  确实被覆盖成目标模板
- 启动日志里的 `Config:   OpenClaw = ...`
  指向的仍然是预期主配置路径

下面是拆开的说明。

先确认环境变量已经准备好：

```bash
export TENCENT_USERNAME='your_name'
export TENCENT_TOKEN='your_token'
```

然后执行：

```bash
cd openclaw
bash ./release.sh publish --version v0.0.46
```

或者一次完成 build 和 publish：

```bash
cd openclaw
bash ./release.sh release --version v0.0.46
```

正式版本默认发布到 `latest/`，等价于显式指定：

```bash
bash ./release.sh release --version v0.0.46 --channel latest
```

preview 版本必须使用 `vX.Y.Z-preview.N` 这类版本号，并显式指定
preview channel：

```bash
bash ./release.sh release \
  --version v0.0.47-preview.1 \
  --channel preview
```

preview 发布只允许更新 `preview/*` 和对应的
`releases/<version>/`，不能更新根 `VERSION` 或 `latest/*`。
如果要把某个 preview 晋级为正式版本，仍然要按正式版本流程重新发布
stable channel，并确保 tag、镜像 `SOURCE_COMMIT` 和 changelog 对齐。

如果你要改 repo 或 path prefix，也可以覆盖：

```bash
bash ./release.sh publish \
  --version v0.0.46 \
  --repo-name trpc-agent-go \
  --path-prefix trpc-claw
```

## 用户安装验证

发布完成后，直接按用户视角验证：

```bash
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
  | bash -s -- --version v0.0.46 --profile mock -f
```

安装完成后再验证：

```bash
trpc-claw inspect plugins
trpc-claw inspect deps --bundled
```

如果这次发布包含默认模板变更、persona 变更、
skills/tooling guidance 变更、或者 `upgrade` 行为变更，
再补一个 CLI 覆盖配置验证：

```bash
trpc-claw upgrade -f --profile wecom-ai-websocket
```

然后检查：

```bash
sed -n '1,120p' ~/.trpc-agent-go/openclaw/openclaw.yaml
trpc-claw upgrade --help
```

如果当前发版版本还不支持 `upgrade -f`，
就继续用安装脚本做覆盖 smoke：

```bash
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
  | bash -s -- -f --profile wecom-ai-websocket
```

如果你要从一个临时 HOME 做 smoke test，可以这样：

```bash
tmp_home="$(mktemp -d)"
HOME="$tmp_home" \
  bash -c "curl -fsSL \
    'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
    | bash -s -- --version v0.0.46 --profile mock"
HOME="$tmp_home" PATH="$tmp_home/.local/bin:$PATH" \
  trpc-claw inspect plugins
HOME="$tmp_home" PATH="$tmp_home/.local/bin:$PATH" \
  trpc-claw inspect deps --bundled
rm -rf "$tmp_home"
```

## 用户安装入口

用户侧只需要这一条：

```bash
curl -fsSL \
  'https://mirrors.tencent.com/repository/generic/trpc-agent-go/trpc-claw/latest/install.sh' \
  | bash
```

这条命令默认会安装 `wecom-ai-websocket` 模板。
如果只是想先本地体验 mock / stdin，再额外带上
`--profile mock`。
