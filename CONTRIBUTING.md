# 参与开发

> 欢迎一起把 Ferry 打磨得更好。动手之前，请先读一遍 README 里的 [边界](README.md#边界)：新功能不能突破“本人账号、本人设备、每次回传都看得见”这三条线。

本文写清怎么在本地跑起来、怎么测试，以及提交、分支与 PR 的规矩；钩子 `.githooks/commit-msg` 与 CI 按同一份规则校验。

## 准备

需要 Go（版本见 `go.mod`）和 Node（前端依赖由 `web/package-lock.json` 锁定）。克隆后执行一次：

```bash
scripts/bootstrap.sh      # 启用提交钩子（core.hooksPath = .githooks），检查工具
scripts/check.sh          # 门禁：与 CI 完全一致；纯文档改动可用 --quick
```

## 本地开发

前后端分开跑，改动即时生效：

```bash
go run ./cmd/ferry serve                         # 后端，默认 :8080
cd web && npm install && npm run dev             # 前端，Vite 把 /api、/sub/、/script/ 转发给后端
FERRY_API=http://127.0.0.1:8098 npm run dev      # 后端不在 8080 时指定地址
```

第一次启动会进入初始化向导，令牌打在后端日志里。

## 测试

| 范围 | 命令 | 说明 |
|---|---|---|
| 后端 | `go test ./...` | CI 只跑 SQLite |
| 三种数据库 | `go test ./internal/store/` | 改了 `internal/store` 时，用专用的空 PostgreSQL、MySQL 库设好 `FERRY_TEST_POSTGRES_DSN`、`FERRY_TEST_MYSQL_DSN` 再跑；测试会删光库里的表，不要指向在用的库 |
| 手机端脚本 | `node --test script/ferry.test.js` | `script/ferry.js` 只用 ES2015；改取值逻辑时同步改 `internal/rules`，并在 `internal/rules/testdata/extract_cases.json` 里加样例，两边共用 |
| 前端 | `cd web && npm run typecheck && npm run lint` | `scripts/check.sh` 已包含 |

文档里的截图来自本机测试实例和伪造数据，不要用真实账号的数据截图。

## 分支与 PR

- `main` 受保护，所有改动走 PR，只允许 squash 合并，合并后删除分支。
- 分支名 `<type>/<slug>`，例如 `feat/export-csv`；slug 只用小写字母、数字、连字符。
- 用 `scripts/pr.sh` 开 PR 并预约自动合并；门禁相关改动用 `scripts/pr.sh --no-merge`。
- PR 标题就是 main 上的提交标题；描述分三节：变更 / 验证 / 未覆盖。

## 提交规范

格式：`<emoji> <type>(<scope>): <中文主题>`

- 首行 ≤ 72 个字符（中文算 1 个），主题动词开头、末尾不加句号。
- scope 可省，只用小写字母、数字和 `._/-`。不兼容改动在冒号前加 `!`，正文写 `BREAKING CHANGE:`。
- **禁止任何署名 trailer**（`Co-authored-by`、`Signed-off-by` 等）。
- 首行不写需求号、阶段号这类流程编号。

| emoji | type | 用途 |
|---|---|---|
| ✨ | feat | 使用者能感知的新功能 |
| 🐛 | fix | 修 bug |
| ⚡️ | perf | 性能优化 |
| ♻️ | refactor | 重构，行为不变 |
| 💄 | style | 不改行为的样式或格式化 |
| ✅ | test | 测试 |
| 📝 | docs | 文档、注释 |
| 📦️ | build | 构建系统、依赖 |
| 👷 | ci | workflow、CI 脚本、钩子 |
| 🔧 | chore | 工程配置、杂项 |
| ⏪️ | revert | 回滚 |
| 🔖 | release | 发布提交 |

本仓扩展类型：无（启用时改 `.githooks/commit.conf` 的 `EXTRA_TYPES`）

常用 scope：`server`、`web`、`ingest`、`store`、`source`、`sub`、`push`、`notify`、`script`、`plans`

钩子是本地第一道检查，CI 会用同一份规则复核 PR 标题和合并进 main 的提交。

## 受管文件

第二行写着「受管文件，来自 ci-workflows@…」的文件（提交钩子、`scripts/ci/` 下的脚本、`scripts/pr.sh` 等）由维护者从共享的规范仓同步过来，各项目保持一致。不要在本仓直接改：要改的话开 Issue，或在 PR 里说明，由维护者改上游后再同步过来。

## 门禁

`scripts/check.sh`（纯文档改动可用 `--quick`）：注释流程编号、shellcheck、actionlint，以及本仓技术栈的检查。

## 发布

推送 `vX.Y.Z` tag 后，`release.yml` 跑完整门禁、构建镜像推到 GHCR，并建 GitHub Release。镜像的 Dockerfile 还在准备中，在那之前不打版本 tag。
版本号与 tag 只由维护者决定和打；CHANGELOG 定稿放在发布 PR（`🔖 release: vX.Y.Z`）里。
