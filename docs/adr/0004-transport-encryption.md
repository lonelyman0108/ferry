# 0004. 数据通道的应用层加密

- 状态：生效（2026-10-24）
- 日期：2026-10-24

## 背景

手机脚本拉规则和回传登录态时用的是明文 JSON，订阅 token 也放在请求头里。链路是否加密完全取决于访问地址是不是 https；局域网直连或临时测试时常用 http，同一网络里的人能读到登录态，拿到 token 后还能冒充手机写入假值。

各代理客户端的脚本环境里没有 WebCrypto，也没有 Node 的 `crypto`，只能用纯 JS 实现加密；`ferry.js` 只能用 ES2015。Go 端已经依赖 `golang.org/x/crypto`（bcrypt）。

## 决策

在 HTTP 之上加一层应用层加密：每个订阅一把服务端生成的 32 字节预共享密钥，算法用 NaCl secretbox（XSalsa20-Poly1305）。请求和回复各用一个子密钥，子密钥取 SHA-512(方向标签 ‖ 密钥) 的前 32 字节。报文格式为 `f1.` + base64url(nonce ‖ 密文)，不带填充。

- Go 端：`internal/transport`，用 `x/crypto/nacl/secretbox`。
- JS 端：把 tweetnacl-js 1.0.3（公有领域）中 secretbox 和 SHA-512 的代码原样摘进 `ferry.js`。
- 两端共用 `internal/transport/testdata/sealed_cases.json`，样例由官方 tweetnacl 生成。

密钥由用户粘贴进客户端的模块参数，不进订阅文件，也不走网络。重放、降级、接口形态等细节见 `docs/plans/传输加密.md`。

## 后果

- 好处：访问地址是 http 时，数据通道也读不到、改不了、冒充不了；拿到订阅文件也拿不到密钥。
- 代价：`ferry.js` 从约 18 KB 增大到约 35 KB；用户要在客户端里多填一个参数；QX 没有模块参数，用不了加密。没有前向保密。
- 边界：订阅文件和脚本本身经 http 下载时仍可能被替换，这层加密防不住，要靠可信的分发通道。
- 后续：摘录的 tweetnacl 代码不手改；升级时整段替换，并重跑共享样例。

## 可推翻条件

- 所有部署都能方便地用上 https 时，可以考虑去掉这一层。
- 客户端普遍提供 WebCrypto 时，改用原生实现，删掉摘录的代码。
- secretbox 出现实际可用的攻击时，换算法和格式前缀。
