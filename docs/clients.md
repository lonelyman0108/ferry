# 客户端差异

> 五个客户端，五套方言。Ferry 只写一份通用脚本 `ferry.js`，把差异收进环境适配层；订阅则按各家的语法分别生成。

本文是这层适配的底稿：订阅生成器（`internal/sub`）和 `ferry.js` 都以它为准。

- 资料来自各家官方文档，查阅日期 2026-10-23；官方没写、来自社区资料的条目标注「社区」，需要真机确认的标注「待验证」。
- Shadowrocket 复用 Surge 的 `.sgmodule`，本文不单列；只有一键安装链接不同（`shadowrocket://install?module=<订阅地址>`）。

## 1. 订阅（模块）格式

| | Surge | Loon | Quantumult X | Stash | Egern |
|---|---|---|---|---|---|
| 文件 | `.sgmodule`（INI） | `.plugin`（INI） | 重写片段 `.snippet`（每行一条） | `.stoverride`（YAML） | 模块 `.yaml` |
| 元信息 | `#!name=`、`#!desc=` | `#!name =`、`#!desc =` 等 | 无 | 顶层 `name`、`desc` | 顶层 `name`、`description` |
| MITM 域名 | `[MITM]` 下 `hostname = %APPEND% a, b` | `[Mitm]` 下 `hostname = a, b` | 片段内 `hostname = a, b`（可选行） | `http.mitm: [a, b]`，默认只对 443 端口 | `mitm.hostnames.includes: [a, b]` |
| 请求脚本 | `[Script]` 下 `名称 = type=http-request,pattern=…,script-path=…` | 旧语法 `http-request <正则> script-path=…`；3.5.1 (983) 起新语法 `request if ${url} ~= /…/ then script("…") with …` | `<正则> url script-request-header <脚本>`；要 body 时用 `script-request-body` | `http.script` 列表项 `match`、`name`、`type: request`，脚本地址在 `script-providers` | `scriptings` 列表项 `http_request: {name, match, script_url}` |
| 需要 body | `requires-body=true` | 旧语法 `requires-body=true`；新语法 `requires_body=true` | 改用 `script-request-body` 类型 | `require-body: true`（没有 s） | `body_required: true` |
| 传参 | `argument=…` → `$argument`（字符串） | 旧语法 `argument="…"`；新语法 `script("…", "…")` → `$argument`（字符串） | 没有 `$argument`：写在脚本 URL 的 `#` 后面，用 `$environment.sourcePath` 自己解析 | `argument: '…'` → `$argument`（字符串） | 原生用 `env` → `ctx.env`；兼容写法 `arguments: {_compat.$argument: "…"}`（社区） |
| 脚本超时默认 | 5 秒 | 旧语法 10 秒；新语法 20 秒 | 未写明 | 由 `timeout`（秒）指定 | 10 秒 |
| body 上限默认 | iOS 1 MB；超出时拒绝连接 | 未写明 | 1024 KB（解压后） | `max-size`，超出时不触发脚本 | 1 MB，超出时不传给脚本 |
| 模块参数（填传输密钥） | `#!arguments=key:` 声明，正文用 `{{{key}}}` 引用；用户填的值随模块实例保存 | `[Argument]` 下 `key = input,"",tag=…`，脚本写 `argument=[{key},…]`，`$argument` 是对象（3.2.1 起） | 没有 | 顶层 `arguments-version: 1` 和 `arguments`，正文用 `${args.key}` 引用；远程更新时保留用户的值（iOS 3.7 起） | 模块 `compat_arguments` 声明默认值，正文用 `{{{key}}}`；用户在主配置的模块引用处用 `compat_arguments` 覆盖 |
| 规则可用策略 | 模块里只能用 DIRECT、REJECT、REJECT-TINYGIF | DIRECT、REJECT 系列、PROXY | 不适用 | 不限 | 不限 |

## 2. 脚本 API

| 能力 | Surge | Loon | Quantumult X | Stash | Egern |
|---|---|---|---|---|---|
| 请求对象 | `$request.url/method/headers/body` | 同 Surge | `$request.url/method/path/headers`；`body` 只在 `script-request-body` 里有 | 同 Surge | 同 Surge（社区；原生 API 是 `ctx`） |
| 放行原请求 | `$done({})`；`$done()` 也放行 | **只能 `$done({})`，`$done()` 会断开请求** | `$done({})` | **只能 `$done({})`，`$done()` 会断开请求** | `$done({})`（待验证） |
| 发请求 | `$httpClient.post(opts, cb)`，`timeout` 单位**秒**，默认 5 | `$httpClient.post(opts, cb)`，`timeout` 单位**毫秒**，默认 5000 | `$task.fetch(opts)` 返回 Promise；默认超时 10 秒 | `$httpClient.post(opts, cb)`，`timeout` 单位秒，默认 5 | `$httpClient`（社区） |
| 本地存储 | `$persistentStore.read/write` | 同 Surge | `$prefs.valueForKey/setValueForKey` | 同 Surge | 同 Surge（社区） |
| 通知 | `$notification.post(t, s, b, {url})` | `$notification.post(t, s, b, {openUrl})` | `$notify(t, s, b, {"open-url"})` | `$notification.post(t, s, b)`，不能带链接 | `$notification.post`（社区） |
| 同一请求多个脚本 | 只运行第一条匹配的 | 只运行第一条匹配的 | 未写明 | 未写明 | 未写明 |

## 3. 安装与更新链接

| 客户端 | 安装链接 | 自动更新 |
|---|---|---|
| Surge | `surge:///install-module?url=<编码后的 URL>` | 已安装模块的刷新条件待验证 |
| Loon | `loon://import?plugin=<编码后的 URL>`；网页可用 `https://www.nsloon.com/openloon/import?plugin=<编码后的 URL>` | 待验证 |
| Quantumult X | `quantumult-x:///add-resource?remote-resource=<编码后的 JSON>`，JSON 形如 `{"rewrite_remote":["<URL>, tag=Ferry"]}` | `rewrite_remote` 行可写 `update-interval=<秒>` |
| Stash | `stash://install-override?url=<编码后的 URL>`；或 `https://link.stash.ws/install-override/<去掉 scheme 的 URL>`，HTTP 地址加 `?scheme=http` | 待验证 |
| Egern | `egern:/modules/new?name=<名称>&url=<URL>`；或 `https://egernapp.com/modules/new?...` | 模块 `update_interval`，默认 86400 秒 |

## 4. 对 Ferry 的影响

1. **放行一律用 `$done({})`。** Loon 和 Stash 里不带参数的 `$done()` 会断开请求，Ferry 出任何错都必须放行原请求。
2. **超时自己控制。** 各家 `$httpClient` 的超时单位不同（Loon 是毫秒），QX 的 `$task.fetch` 不能单独设超时。适配层统一用 `setTimeout` 计时，3 秒到了就放行，不依赖各家的 timeout 参数。
3. **传参方式要兼顾 QX。** QX 没有 `$argument`，只能把参数写在脚本 URL 的 `#` 后面，用 `$environment.sourcePath` 读出来；Egern 原生用 `ctx.env`。候选做法是：各客户端都用 `$argument` 传参，QX 单独用 `#` 片段。其他客户端会不会把 `#` 片段原样留在脚本地址里，待验证；阶段 4 写适配层时再定。
4. **body 开关的名字各不相同**（`requires-body` / `requires_body` / `require-body` / `body_required`，QX 换脚本类型）。生成器按客户端分别输出。在 QX 里要从 body 取值的匹配规则，生成 `script-request-body`；只从请求头取值的，生成 `script-request-header`。
5. **同一请求只跑一个脚本（Surge、Loon）。** 如果别的模块也匹配了同一个 URL，Ferry 的脚本可能不运行。匹配规则的正则要尽量精确，并在文档里提醒使用者。Surge 模块的 `[Script]` 行会插到最前面，多个模块之间谁先谁后，待验证。
6. **Egern 生成原生 YAML。** Egern 能加载 `.sgmodule`，但两者的字段并不完全对应，所以 Egern 单独生成原生 YAML，不复用 sgmodule。
7. **点击通知打开链接，各家字段不同**（`url` / `openUrl` / `open-url`），Stash 不支持。MITM 变化的提醒由平台从服务端发（Bark 等），不依赖手机脚本的通知。
8. **请求头大小写不统一。** 取值时请求头名按不区分大小写匹配。

## 5. 待验证（阶段 0 真机）

- [ ] Surge：模块里的 `DEVICE:` 规则是否生效；`$httpClient` 的 `policy` 能否指定 `DEVICE:`
- [ ] Surge：通过安装链接重复安装同一 URL 时，是否覆盖更新原模块
- [ ] Surge、Loon、Stash：已安装的模块 / 插件 / 覆写多久自动刷新一次
- [ ] Egern：在原生 YAML 模块里是否能跑 `$request`、`$httpClient`、`$persistentStore`、`$done` 写法的脚本
- [ ] QX：`script-request-body` 遇到空 body 的请求（如 GET）还会不会执行
- [ ] 各客户端：请求头名在 `$request.headers` 里是原样还是小写
- [ ] Surge、Loon、Stash、Egern：脚本地址带 `#` 片段时，能否正常下载并运行
- [ ] Surge（iOS）：`#!arguments` 的参数表能否在手机上编辑；更新模块后已填的 `key` 是否保留
- [ ] Shadowrocket：是否支持 `#!arguments` 和 `{{{key}}}`（不支持时脚本按“没填密钥”处理，只提示、不回传）
- [ ] Loon：`[Argument]` 的默认值改了以后（如平台地址变化），更新插件时是否跟着变
- [ ] Egern：界面上能否编辑 `compat_arguments`，还是只能改配置文件
- [ ] 各客户端脚本环境里有没有 `crypto.getRandomValues`（没有时用时间加计数器拼 nonce）

## 资料来源

- Surge：[Module（参数表）](https://manual.nssurge.com/profile/module.html)、[Scripting Basics](https://manual.nssurge.com/scripting/common.html)、[HTTP Request Script](https://manual.nssurge.com/scripting/http-request.html)、[JavaScript API](https://manual.nssurge.com/scripting/api.html)、[Module](https://manual.nssurge.com/others/module.html)、[URL Scheme](https://manual.nssurge.com/others/url-scheme.html)
- Loon：[插件](https://loon0x00.github.io/docs/Plugin/)、[Script（旧语法）](https://loon0x00.github.io/docs/Script/)、[Script（新语法）](https://nsloon.app/docs/Script/script_v2)、[Script API](https://loon0x00.github.io/docs/Script/script_api/)、[Scheme](https://nsloon.bid/document/scheme)
- Quantumult X：[sample.conf](https://github.com/crossutility/Quantumult-X/blob/master/sample.conf)、[sample-task.js](https://github.com/crossutility/Quantumult-X/blob/master/sample-task.js)、[url-scheme.md](https://github.com/crossutility/Quantumult-X/blob/master/url-scheme.md)
- Stash：[改写 HTTP](https://stash.wiki/script/rewrite-requests)、[语法与接口](https://stash.wiki/script/syntax-and-interface)、[覆写文件](https://stash.wiki/configuration/override)、[URL Schema](https://stash.wiki/faq/url-schema)
- Egern：[Scripting](https://egernapp.com/docs/configuration/scriptings/)、[Modules](https://egernapp.com/docs/configuration/modules/)、[URL Scheme](https://egernapp.com/docs/url-scheme/)；兼容写法参考 [Sub-Store 的 Egern 模块](https://github.com/sub-store-org/Sub-Store/blob/master/config/Egern.yaml)（社区）
