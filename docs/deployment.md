# 部署与配置

> Ferry 是一个二进制、一个端口、一个数据库。部署的全部功课，是让手机够得着它，并把主密钥和数据库分开保管好。

## 运行

预构建的镜像与二进制还在准备中。在那之前，从源码构建带管理界面的二进制：

```bash
(cd web && npm ci && npm run build)
go build -tags embed_ui -o ferry ./cmd/ferry
./ferry serve -config ferry.yaml        # 也可以用 -addr 覆盖监听地址
./ferry version
```

不带 `embed_ui` 编译出的二进制没有管理界面，打开首页只显示一张提示页。

第一次启动时没有配置，日志会打印一次性的初始化令牌；打开管理界面，按向导选数据库、生成主密钥、创建管理员。初始化完成后，向导接口不再可用。

## 配置项

配置来自 `ferry.yaml`，环境变量会覆盖文件里的同名项；容器部署时可以只用环境变量、不写配置文件。文件和环境变量都没给出数据库和主密钥时，进入初始化向导。

| 配置文件 | 环境变量 | 默认值 | 说明 |
|---|---|---|---|
| — | `FERRY_CONFIG` | `ferry.yaml` | 配置文件路径，也可以用 `serve -config` 指定 |
| `listen` | `FERRY_LISTEN` | `:8080` | 监听地址，可被 `serve -addr` 覆盖 |
| `database.driver` | `FERRY_DATABASE_DRIVER` | — | `sqlite`、`postgres` 或 `mysql` |
| `database.dsn` | `FERRY_DATABASE_DSN` | — | 连接串；SQLite 是文件路径 |
| `master_key` | `FERRY_MASTER_KEY` | — | base64 编码的 32 字节主密钥，向导会自动生成 |

同一个进程同时提供管理界面、管理 API（`/api/v1/*`）、订阅文件（`/sub/{token}/{file}`）、通用脚本（`/script/ferry.js`）和健康检查（`/healthz`）。

## 让手机够得着

- 在 **设置 → 访问地址** 里填手机可达的地址，可以有多个（比如局域网与公网），订阅可以指定主地址和备用地址。
- 经反向代理挂在子路径下时，访问地址要带上路径。
- 访问地址是 http 时，建议给订阅开启 [传输加密](security.md)；Quantumult X 不支持加密，给它用 https 地址。
- 在 macOS 上自己编译运行时，访问局域网里的其他主机（比如青龙）可能报 `no route to host`：这是 macOS 的“本地网络”权限拦住了自己编译的程序。可以用 SSH 本地端口转发绕开（`ssh -f -N -L 127.0.0.1:15700:127.0.0.1:5700 <青龙所在主机>`，目的地填 `http://127.0.0.1:15700`）；部署在 Linux 上没有这个问题。

## 备份

- **数据库与主密钥一起才完整**：主密钥（配置里的 `master_key`，或环境变量 `FERRY_MASTER_KEY`）丢了，敏感值、订阅令牌和目的地配置都无法解密。
- **但要分开保管**：两者放在一起泄露，加密就形同虚设。
- 版本有保留策略：每个“身份 + 字段”至少保留最近 50 个版本，180 天内的全部保留，当前值永远保留；每天自动清理一次。
