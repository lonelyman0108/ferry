# Ferry

个人登录态同步工具：在自己的手机代理客户端（Surge / Loon / Quantumult X / Stash / Egern）里读取本人账号的登录态，Ferry 保存版本、做校验后，同步给自己的自动化任务（如青龙）。技术栈：Go 单二进制（内嵌 Vue 3 + shadcn-vue 前端），GORM（SQLite / PostgreSQL / MySQL）。

## 使用范围（设计约束）

Ferry 是**自用**的个人自动化工具：把**本人账号**在**本人设备**上的登录态（App 自己发出的 token、Cookie 等）同步给**本人**的自动化任务（如青龙里的签到脚本），省去每次登录态过期后手动复制。

- 只同步使用者自己的账号；来源由使用者本人在后台逐个配置。
- 只在使用者自己的设备上运行：订阅需要使用者在自己的代理客户端里手动安装，并信任自己生成的证书。
- 不做静默采集：每次回传都会在手机上弹出可见通知（默认开启），后台保留完整的审计记录。
- 不面向第三方设备或他人账号，也不提供任何隐藏、绕过或扩散的能力。

新功能不得突破以上范围；涉及默认关闭手机通知、隐藏运行之类的需求，一律先和维护者确认。

提交、PR、发版的完整规范见 [CONTRIBUTING.md](CONTRIBUTING.md) 与 ci-workflows 的 `docs/handbook/02-项目规范.md`；本文件只写 agent 需要、又无法从代码推出来的内容，每条约定都附原因。超过 300 行时拆到子目录的 AGENTS.md。

## 工作方式

- 只在分支或 worktree 上工作，分支名 `<type>/<slug>`；用 `scripts/pr.sh` 开 PR，**不直接提交或推送 main**。原因：main 受保护，PR 是唯一经过门禁的入口。
- 提交与 PR 标题：`<emoji> <type>(<scope>): <中文主题>`，首行 ≤ 72 字符；类型表和本仓扩展见 CONTRIBUTING.md「提交规范」。钩子会拦下不合规的提交。
- **不加任何署名 trailer**（Co-authored-by 等）。原因：规范禁止，钩子与 CI 两道都拦。
- 注释只讲代码本身，不写需求号、阶段号、验收号这类流程编号；这些编号只留在 `docs/` 的过程文档。`scripts/lint/comments.sh` 会检查。
- 提交前跑 `scripts/check.sh`（纯文档改动可用 `--quick`），结果写进 PR 的「验证」节。
- 一次性的进度写进 `docs/plans/` 的计划文档，不写进本文件。

## 禁区

原因：这些操作影响发布、平台或门禁本身，必须由维护者本人执行。

- 不打 tag、不改版本号、不运行不带 `--dry-run` 的发布脚本、不上传、不部署。
- 不改 GitHub 平台设置（secrets、规则集、Actions 权限），不读取或打印密钥文件（`~/.config/*/token`、`.env`、钥匙串）。
- 不用 `--no-verify`、`push --force`。
- 改 `.githooks/`、`.github/workflows/`、`scripts/check.sh`、`scripts/ci/`、发布脚本时，只开 PR（`scripts/pr.sh --no-merge`），由维护者审阅后合并。
- 无人值守任务一律 `scripts/pr.sh --no-merge`。
- `.githooks/commit-msg`、`.githooks/pre-commit`、`scripts/ci/*`、`scripts/lint/comments.sh`、`scripts/pr.sh` 是 ci-workflows 的副本（第二行写着来源版本），不在本仓直接改：先改上游，再同步过来。

## 命令

- 门禁：`scripts/check.sh`（`--quick` 只跑通用检查）
- 初始化：`scripts/bootstrap.sh`
- 开 PR：`scripts/pr.sh`（`--no-merge` 只开不合并）
- 三种数据库的存储测试：CI 只跑 SQLite。改了 `internal/store` 时，在本机起临时的 PG / MySQL 容器，设 `FERRY_TEST_POSTGRES_DSN`、`FERRY_TEST_MYSQL_DSN` 后跑 `go test ./internal/store/`。这两个 DSN 必须指向专用的空库，因为测试会删掉库里的全部表；不要指向任何在用的数据库。
- 手机端脚本：`script/ferry.js` 只用 ES2015（各客户端的 JS 引擎版本不一），测试是 `node --test script/ferry.test.js`。改取值逻辑时要同时改 `internal/rules`，并在 `internal/rules/testdata/extract_cases.json` 里加样例，两边共用。
- 带界面的构建：`(cd web && npm run build) && go build -tags embed_ui -o ferry ./cmd/ferry`。不带 `embed_ui` 时二进制里没有界面（go vet / go test 时 `web/dist` 可能还没构建，所以默认不内嵌）。
- 本地运行：后端 `go run ./cmd/ferry serve`（默认 `:8080`）；前端 `cd web && npm run dev`，Vite 把 `/api` 转发给后端

## 目录与约定

| 路径 | 内容 |
|---|---|
| `scripts/check.sh` | 唯一门禁入口，本地与 CI 行为一致 |
| `scripts/ci/` | 只被 CI 调用的小脚本 |
| `docs/plans/` | 计划文档，一个主题一份，持续勾选 |
| `docs/adr/` | 长期决策记录 |
| `cmd/ferry` | 程序入口，子命令 `serve`、`version` |
| `internal/` | 后端各模块，外部不可引用 |
| `web/` | 管理界面（Vue 3 + Vite + shadcn-vue），用 npm 和 `package-lock.json` |
| `web/src/components/ui` | shadcn-vue 生成的组件源码。用 `npx shadcn-vue@2.8.2 add <组件>` 添加，不手改样式；add 会把已有依赖改回 `^` 版本，加完要还原 `package.json` 和锁文件（有新依赖就改成固定版本）；CLI 本身不进依赖，它的 `tailwind.css` 复制成了 `src/shadcn-tailwind.css` |
| `script/` | 手机端通用脚本 `ferry.js` |

<!-- 追加本仓的目录约定 -->

## 已知的坑

- 在 Mac 上跑 ferry 时访问局域网其他主机（如局域网里的青龙）会报 `no route to host`：macOS 的“本地网络”权限拦住了自己编译的程序。测试时用 SSH 本地端口转发（`ssh -f -N -L 127.0.0.1:15700:127.0.0.1:5700 <青龙所在主机>`），目的地地址填 `http://127.0.0.1:15700`；部署在 Linux 的 Docker 里没有这个问题。
- `web/node_modules` 里有的包自带 Go 代码（如 flatted），会被 `go vet ./...` 扫进去。`go.mod` 里用 `ignore ./web/node_modules` 排除，不要删这一行。
