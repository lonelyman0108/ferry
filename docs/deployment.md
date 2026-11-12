# 部署与配置

> Ferry 是一个二进制、一个端口、一个数据库。部署的全部功课，是让手机够得着它，并把主密钥和数据库分开保管好。

## 用 Docker 运行

镜像同时提供 `linux/amd64` 与 `linux/arm64`，两处都能拉：`ghcr.io/lonelyman0108/ferry`、`lonelyman0108/ferry`（Docker Hub）。标签有 `0.1.0`、`0.1` 和 `latest`，部署时建议写死具体版本。

最简单的一行：

```bash
docker run -d --name ferry -p 8080:8080 -v ferry-data:/data ghcr.io/lonelyman0108/ferry:0.1.0
docker logs ferry          # 找 setup_token=… 这一行，向导要用
```

或者用仓库里的 compose 示例：

| 文件 | 适合 |
|---|---|
| [`deploy/compose.yaml`](../deploy/compose.yaml) | SQLite，一个容器搞定 |
| [`deploy/compose.postgres.yaml`](../deploy/compose.postgres.yaml) | 搭配 PostgreSQL，数据库连接由 `FERRY_DATABASE_URL` 给出，向导里自动回填 |

镜像里的约定：

- **数据都在 `/data`**：配置文件是 `/data/ferry.yaml`；向导里 SQLite 填相对路径（默认 `ferry.db`）时，文件也落在这里。挂一个卷就能把全部状态留住。
- **以非 root 用户（65532）运行**。命名卷会沿用镜像里 `/data` 的属主，开箱即用；改成目录挂载时，先 `mkdir -p data && sudo chown 65532:65532 data`。
- **自带健康检查**：容器里运行 `ferry healthcheck`，请求本机的 `/healthz`。
- **升级**：把镜像标签改成新版本，`docker compose pull && docker compose up -d`；数据库结构会在启动时自动补齐。系统信息页会提醒你有新版本。

## 用二进制运行

每个 [Release](https://github.com/lonelyman0108/ferry/releases) 都附有 linux、darwin 各 amd64、arm64 的压缩包与 `SHA256SUMS`，解开即用：

```bash
./ferry serve -config ferry.yaml        # 也可以用 -addr 覆盖监听地址
./ferry version
```

想自己从源码构建：

```bash
(cd web && npm ci && npm run build)
go build -tags embed_ui -o ferry ./cmd/ferry
```

不带 `embed_ui` 编译出的二进制没有管理界面，打开首页只显示一张提示页。

第一次启动时，日志会打印一次性的初始化令牌；打开管理界面，按向导选数据库、生成主密钥、创建管理员。初始化完成后，向导接口不再可用。

## 配置项

> **配置文件优先。** 配置文件里有的项以文件为准，文件里没有的才用环境变量补上，监听地址最后用默认值兜底。系统信息页会标出每一项实际来自哪里。

| 配置文件 | 环境变量 | 默认值 | 说明 |
|---|---|---|---|
| — | `FERRY_CONFIG` | `ferry.yaml`（镜像里是 `/data/ferry.yaml`） | 配置文件路径，也可以用 `serve -config` 指定 |
| `listen` | `FERRY_LISTEN` | `:8080` | 监听地址，可被 `serve -addr` 覆盖 |
| `database.driver` | `FERRY_DATABASE_DRIVER` | — | `sqlite`、`postgres` 或 `mysql` |
| `database.dsn` | `FERRY_DATABASE_DSN` | — | 连接串；SQLite 是文件路径 |
| — | `FERRY_DATABASE_URL` | — | 一个变量同时给出驱动和连接串，见下文；和上面两项同时给出时以上面两项为准 |
| `master_key` | `FERRY_MASTER_KEY` | — | base64 编码的 32 字节主密钥，向导会自动生成 |

**什么时候进向导**：数据库驱动、连接串、主密钥三项缺任何一项，启动后都进初始化向导，而不是报错退出。已经给出的那几项会回填进向导：连接串里的密码显示为 `••••`，不改就沿用原值；改了就写进配置文件，从此以文件为准。

**`FERRY_DATABASE_URL` 的写法**：

| 数据库 | 例子 |
|---|---|
| PostgreSQL | `postgres://ferry:密码@db:5432/ferry?sslmode=disable` |
| MySQL | `mysql://ferry:密码@db:3306/ferry`（自动补上 `charset=utf8mb4&parseTime=True&loc=UTC`） |
| SQLite | `sqlite:///data/ferry.db`（三个斜杠后跟绝对路径） |

**主密钥来自环境变量时**，向导沿用它、不再生成，也**不会把它写进配置文件**。这样磁盘上只有一份密钥；代价是以后去掉这个环境变量，Ferry 会因为缺主密钥回到向导——到那时请把同一把密钥写进配置文件的 `master_key`。

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
