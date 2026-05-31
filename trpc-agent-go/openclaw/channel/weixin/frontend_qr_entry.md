# AGUI 前端接入微信二维码入口

这份文档专门写给前端同学。

目标只有一个：

- 你已经拿到了某个运行中 `trpc-claw` 的 `admin_url`
- 你想给用户一个“点开就能去扫码”的入口
- 你不想理解微信二维码登录协议
- 你也不想自己维护二维码 session

如果你只记一件事，就记这个：

- 前端要打开的是 `trpc-claw admin` 的固定入口 URL
- 不是微信二维码链接本身

## 1. 先理解这条链路里谁负责什么

在微信登录链路里，有三类地址：

1. AGUI 自己的页面地址
2. `trpc-claw` admin 地址
3. 微信二维码页面地址

你们前端真正需要稳定依赖的是第 2 类，也就是：

- `admin_url`

微信二维码页面地址不是稳定规则地址。
它是 `trpc-claw` 先调用微信接口之后，动态拿到的一个值。

所以正确分工是：

- AGUI 前端：
  - 只负责打开一个固定的 admin 入口 URL
- `trpc-claw`：
  - 决定要不要自动开始二维码登录
  - 持有当前登录 session
  - 等二维码准备好
  - 再把浏览器跳到微信二维码页面
- 微信：
  - 承载最终给用户看的二维码页面

## 2. 你们应该打开哪个 URL

固定入口是：

```text
{admin_url}/channels/wx_qr
```

如果你已经明确知道目标 runtime，也支持显式传：

```text
{admin_url}/channels/wx_qr?runtime_key=weixin-1
```

例子：

```text
https://test-compile-cloud.woa.com/<proxy-prefix>/proxy/19789/channels/wx_qr
```

这里的 `<proxy-prefix>` 由你们现有的容器 / WebIDE / proxy 逻辑决定。
前端不需要自己拼微信二维码地址，只需要继续沿用你们现在拼
`admin_url` 的办法，再在后面追加 `/channels/wx_qr`。

## 3. 打开这个 URL 之后会发生什么

浏览器访问 `/channels/wx_qr` 后，`trpc-claw` 会按下面的顺序处理：

1. 找到目标 Weixin runtime
2. 如果当前没有已保存账号，而且当前也没有活跃登录 session，
   自动开始一次二维码登录
3. 如果已经有活跃登录 session，就直接复用
4. 如果二维码页面地址已经拿到了，直接把浏览器重定向过去
5. 如果二维码还没准备好，先返回一个很薄的等待页；
   这个等待页会自动刷新，直到能跳到微信二维码页面

也就是说，前端拿到的不是二维码图片二进制，也不是微信页面的 HTML
源码，而是：

- 先打开 admin 固定入口
- 再由 admin 自己把浏览器导向微信二维码页面

## 4. 什么情况下它不会自动开始新的二维码登录

为了避免误触发重新绑定，`/channels/wx_qr` 有一个保护行为：

- 如果当前 runtime 已经有保存好的 Weixin 账号
- 并且当前没有活跃登录 session

那它不会偷偷帮你重新开始一次新的二维码登录。

这种情况下，它会显示一个明确的“已经绑定”页面，并引导回
`/channels`。

这是故意的。
这样不会因为前端误点或页面重复打开，就把一个已经可用的 runtime
拉进新的绑定流程。

## 5. 前端应该怎么打开

最推荐的方式是：

- 用普通浏览器导航打开
- 最好在用户点击事件里直接新开页

例如：

```ts
window.open(`${adminUrl}/channels/wx_qr`, "_blank", "noopener,noreferrer");
```

或者：

```tsx
<a
  href={`${adminUrl}/channels/wx_qr`}
  target="_blank"
  rel="noreferrer"
>
  绑定微信
</a>
```

为什么推荐直接打开，而不是 `fetch`：

- 浏览器页面跳转不依赖前端去读取跨域响应体
- 这条链路的目标本来就是把用户带到二维码页面
- 你们不需要把中间态 JSON 拉回自己页面再二次处理

## 6. 会不会有跨域问题

### 6.1 直接打开页面

如果前端只是：

- `window.open(...)`
- `window.location = ...`
- `<a href="...">`

这种浏览器导航方式，一般不把 CORS 当成主问题。

因为 CORS 主要限制的是：

- 前端 JS 用 `fetch` / `XMLHttpRequest` 去读另一个域名的响应体

而这里更接近：

- 浏览器正常打开一个 URL
- admin 再把浏览器跳到微信二维码页面

### 6.2 不推荐的方式

第一版不要默认走下面这些方式：

- `fetch(admin_url + "/channels/wx_qr")`
- `iframe` 直接嵌微信二维码页面

原因是：

- `fetch` 会把你们拉回 CORS 语义
- `iframe` 可能被对方页面的嵌入策略挡掉
- 这两种方式都比直接打开新页面复杂很多

## 7. 二维码过期怎么办

微信二维码页面本身是会过期的。

当前固定入口的设计是：

- 它负责把浏览器送到“当前最新二维码页面”
- 但用户一旦已经跳到微信页，后续就不再停留在 admin 页面里

所以如果用户在微信页里看到二维码已经失效，最简单的处理是：

1. 关闭当前二维码页
2. 再次打开同一个固定入口 URL：

```text
{admin_url}/channels/wx_qr
```

`trpc-claw` 会复用当前活跃 session，
或者在需要时继续推进到新的二维码页面。

## 8. 多个 Weixin runtime 怎么办

第一版固定入口的默认约定是：

- 没传参数时，只适合“当前实例里只有一个 Weixin runtime”的情况

如果同一个 `trpc-claw` 实例里配置了多个 Weixin runtime，
不带参数访问：

```text
{admin_url}/channels/wx_qr
```

会得到一个明确错误页，让用户回到 `/channels` 选择具体 runtime。

内部管理页面里会给每个 runtime 都渲染专属入口链接；
这些链接的形式就是：

```text
{admin_url}/channels/wx_qr?runtime_key=<runtime_key>
```

如果以后 AGUI 真要支持多 runtime 选择，再单独补 runtime 选择逻辑。

## 9. 前端最小接入步骤

假设你们已经有：

- 创建 CIC 容器的逻辑
- 拉起 `trpc-claw` 的逻辑
- 轮询容器 ready 的逻辑
- 从后台拿到 `admin_url` 的逻辑

那前端最小接入只需要：

1. 等容器 ready
2. 用户点击“绑定微信”
3. 前端打开：

```text
{admin_url}/channels/wx_qr
```

就够了。

前端第一版不需要：

- 请求微信接口
- 自己构造二维码 URL
- 自己保存登录状态
- 自己轮询二维码 session

## 10. 后台如何知道已经绑定成功

不要等微信二维码页“回调” AGUI。

更稳妥的方式是：

- AGUI 前端负责打开 `/channels/wx_qr`
- AGUI 后台轮询 `trpc-claw admin` 自己的状态接口
- 用 `trpc-claw` 已经拿到的登录结果，判断“是否已绑定”

原因很简单：

- 浏览器最后停留的是微信自己的二维码页面
- 这个页面不是我们控制的
- 但 `trpc-claw` 自己一直在轮询微信登录状态
- 所以“扫码是否完成”这件事，`trpc-claw` 后端其实是知道的

### 10.1 首选接口：`/api/weixin/status`

如果你只关心 Weixin 绑定状态，优先用：

```text
GET {admin_url}/api/weixin/status
```

这是 Weixin 专用状态接口。

返回结果里最重要的字段有：

- `runtimes[]`
  - 每个 Weixin runtime 一条记录
- `runtimes[].key`
  - runtime key，比如 `weixin-1`
- `runtimes[].account_count`
  - 当前已保存账号数
- `runtimes[].login.status`
  - 最近一次二维码登录状态
- `runtimes[].login.saved_account_id`
  - 最近一次扫码成功后保存的账号 ID
- `runtimes[].accounts[]`
  - 当前 runtime 已保存的账号列表
- `runtimes[].accounts[].account_id`
  - 账号 ID
- `runtimes[].accounts[].user_id`
  - 微信 user ID
- `runtimes[].accounts[].state`
  - 当前账号状态，常见值是 `ready` / `paused`

一个裁剪后的例子：

```json
{
  "auto_refresh": false,
  "refresh_interval_ms": 5000,
  "runtimes": [
    {
      "key": "weixin-1",
      "title": "Weixin Runtime",
      "account_count": 1,
      "login": {
        "exists": true,
        "active": false,
        "status": "confirmed",
        "saved_account_id": "wx-bot-123"
      },
      "accounts": [
        {
          "account_id": "wx-bot-123",
          "user_id": "user_a@im.wechat",
          "state": "ready"
        }
      ]
    }
  ]
}
```

`login.status` 常见值：

- `starting`
- `wait`
- `scaned`
- `scaned_but_redirect`
- `confirmed`
- `expired`
- `cancelled`
- `failed`

注意：

- `scaned` 这个拼写是历史兼容字段，不是文档笔误

### 10.2 备选接口：`/api/channels/status`

如果你的页面本来就在消费 Channels 总览接口，也可以直接用：

```text
GET {admin_url}/api/channels/status
```

它会把同一份 Weixin runtime 信息放在：

- `weixin[]`

一个裁剪后的例子：

```json
{
  "generated_at": "2026-04-22 15:20:00 CST",
  "weixin": [
    {
      "key": "weixin-1",
      "account_count": 1,
      "login": {
        "status": "confirmed",
        "saved_account_id": "wx-bot-123"
      },
      "accounts": [
        {
          "account_id": "wx-bot-123",
          "user_id": "user_a@im.wechat",
          "state": "ready"
        }
      ]
    }
  ]
}
```

怎么选：

- 只关心微信绑定状态：优先 `/api/weixin/status`
- 页面已经在用 Channels 总览：可以直接复用

### 10.3 前端 / 后台最小判定规则

如果你们只是想回答“现在到底绑没绑好”，用下面这套规则就够了：

1. 先找到目标 runtime
2. 如果 `login.status == "confirmed"`，可以认为最近一次扫码已经成功
3. 如果 `accounts.length > 0`，可以认为当前 runtime 已经有保存账号
4. 如果存在某个 `accounts[i].state == "ready"`，可以认为这个
   Weixin runtime 已可用

最实用的组合判断通常是：

- “已绑定”：
  `login.status == "confirmed" || accounts.length > 0`
- “已可用”：
  `accounts.some(a => a.state === "ready")`

### 10.4 多个 runtime 时怎么对应

如果同一个 `trpc-claw` 实例里有多个 Weixin runtime，
后台不要只看“有没有任意一个 runtime 绑定成功”。

更稳妥的做法是：

1. 前端打开二维码入口时就明确带上：

```text
{admin_url}/channels/wx_qr?runtime_key=weixin-1
```

2. 后台轮询状态接口时，也按同一个 `runtime_key` 去匹配

也就是说：

- 打开哪个 runtime 的二维码入口
- 就检查哪个 runtime 的状态

不要把不同 runtime 的绑定状态混在一起。

### 10.5 建议的轮询方式

推荐一个足够简单的第一版：

1. 用户点击“绑定微信”
2. 前端打开 `/channels/wx_qr`
3. AGUI 后台开始每 `2` 到 `3` 秒轮询一次
   `/api/weixin/status`
4. 一旦看到目标 runtime 满足“已绑定”或“已可用”，就更新你们自己的
   任务状态
5. 达到成功态后停止高频轮询

一个很薄的伪代码例子：

```ts
async function pollWeixinBinding(adminUrl: string, runtimeKey: string) {
  const rsp = await fetch(`${adminUrl}/api/weixin/status`);
  const data = await rsp.json();

  const runtime = data.runtimes?.find(
    (item: { key?: string }) => item.key === runtimeKey,
  );
  if (!runtime) {
    return { bound: false, ready: false };
  }

  const accounts = Array.isArray(runtime.accounts) ? runtime.accounts : [];
  const bound =
    runtime.login?.status === "confirmed" || accounts.length > 0;
  const ready = accounts.some(
    (account: { state?: string }) => account.state === "ready",
  );

  return { bound, ready, runtime };
}
```

### 10.6 不建议依赖什么

不要把下面这些条件当成“绑定成功”的唯一判断：

- 浏览器是不是已经跳到微信页
- 用户是不是手动关闭了二维码页
- 二维码 URL 本身是不是还能打开

这些都不如 `trpc-claw admin` 自己的状态可靠。

## 11. 现有动作接口能做到什么

除了状态查询，当前 `trpc-claw admin` 还已经提供了几类动作接口。

### 11.1 解绑

如果前端已经拿到了目标 runtime 和 account ID，
可以调用：

```text
POST {admin_url}/api/weixin/accounts/remove
```

表单字段：

- `runtime_key`
- `account_id`

这个动作会把对应账号从状态目录里移除，
包括：

- account 文件
- cursor
- context token
- runtime status

这更接近“解绑”。

### 11.2 恢复一个被 pause 的账号

如果账号因为微信后端状态异常被标成 `paused`，
可以调用：

```text
POST {admin_url}/api/weixin/accounts/resume
```

表单字段：

- `runtime_key`
- `account_id`

什么时候适合展示“恢复”按钮：

- `accounts[i].state == "paused"`
- `accounts[i].can_resume == true`

### 11.3 手动重新发起一次二维码登录

如果前端想显式触发一次新的二维码登录 session，
可以调用：

```text
POST {admin_url}/api/weixin/login/start
```

表单字段：

- `runtime_key`
- `base_url`
- `bot_type`

其中：

- `base_url`
  不传时用 runtime 默认值
- `bot_type`
  不传时用默认值

登录开始后，再继续轮询：

- `GET {admin_url}/api/weixin/status`

等到：

- `login.qr_code_url` 出现

就可以把用户带到最新二维码页。

如果前端不想自己拿 `qr_code_url`，
也可以在启动登录后直接打开：

```text
{admin_url}/channels/wx_qr?runtime_key=<runtime_key>
```

### 11.4 取消当前登录 session

如果前端需要取消一个已经开始但还没完成的扫码流程，
可以调用：

```text
POST {admin_url}/api/weixin/login/cancel
```

表单字段：

- `runtime_key`

### 11.5 这些动作接口的一个限制

这些动作接口当前是给 admin 页面表单直接使用的。

所以它们的成功返回不是 JSON，
而是：

- `303 See Other`

并带一个跳回 `/channels` 的重定向。

这意味着：

- 浏览器表单直接提交是没问题的
- AGUI 后台如果要调，也可以调
- 但它更像“admin action endpoint”，还不是特别干净的
  “frontend JSON API”

如果 AGUI 要接，第一版可以直接用；
只是接入层要接受：

- `application/x-www-form-urlencoded`
- `303` 重定向语义

## 12. 现有接口是否已经足够

先说结论：

- “解绑”：基本已经够了
- “恢复 / 重新扫码”：基本已经够了
- “换绑”：只有组成零件，还没有一个明确的一步式语义
- “被其他助手挤占”的产品级提示：能感知，但还不是最稳的机器可读协议

具体来说：

### 12.1 已经足够的部分

如果 AGUI 只是想做下面这些能力：

- 展示当前绑定了哪些微信账号
- 展示账号是否 `ready` / `paused`
- 做“解绑”
- 做“恢复”
- 做“重新发起扫码”

那当前接口已经能支撑，
主要就是：

- 状态接口
- remove
- resume
- login start

这部分确实更偏“补文档，前端对接”。

### 12.2 还不够干净的部分

如果 AGUI 想做得像产品截图那样更完整，
当前接口还有两个明显缺口。

第一个缺口是“换绑”的语义还不够明确。

原因是当前 Weixin runtime 本来就支持：

- 多账号并行

所以：

- `login/start`
  更像“再绑定一个账号”
- 不天然等于“替换掉当前唯一绑定账号”

也就是说，“换绑”到底是：

1. 先解绑旧账号，再让用户扫新账号
2. 先扫新账号，成功后再删旧账号
3. 只允许保留一个账号，自动替换

这些策略现在都不是一个单独 API 的固定语义。

第二个缺口是“被挤占”的原因还不是完全机器可读。

现在前端已经能看到：

- `state == "paused"`
- `last_error`
- `can_resume`

这足够做一个基本提示。

但如果你想非常稳定地渲染成：

- “微信渠道已被其他助手挤占，请重新绑定”

那当前更接近：

- 根据 `paused` 和错误信息做产品解释

而不是直接拿一个稳定的结构化 reason code。

### 12.3 我更推荐的判断

所以我更推荐把结论分成两层：

1. 先用现有接口把“解绑 / 恢复 / 重新扫码 / 状态展示”接起来
2. 如果 AGUI 确实要上线“一键换绑”与“稳定挤占提示”，
   再补一版更明确的 API 语义

一个比较自然的后续增强方向是：

- 给状态接口补结构化 reason 字段
- 给动作接口补 JSON 返回模式
- 如果产品确认了“换绑”策略，再补一个明确的 rebind API

## 13. 不要做什么

不要做下面这些事情：

- 不要尝试从 `admin_url` 推导微信二维码地址
- 不要把微信二维码地址当成稳定协议字段写死
- 不要把二维码 session 状态搬到 AGUI 前端维护
- 不要默认走跨域 `fetch`
- 不要把已经可用的 runtime 静默重登

## 14. 你们如何验证自己接对了

前端只要能稳定做到这几件事，就说明接法是对的：

1. 用户点击按钮后，能打开 `/channels/wx_qr`
2. 页面会自动跳到微信二维码页，或者先看到一个短暂等待页
3. 用户扫码完成后，`/channels` 里能看到账号数量从 `0` 变成 `1`
4. 不需要前端知道 `qr_code_url`

## 15. 相关文档

- 插件说明：
  [`README.md`](README.md)
- 本地手工跑通示例：
  [`get-started.md`](get-started.md)
