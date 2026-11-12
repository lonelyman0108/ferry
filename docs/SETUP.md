# ferry 平台与环境设置

平台设置一改，就在同一个 PR 里更新本文。这里只写密钥的**名字**和用途，不写值。

## 仓库设置

| 项 | 值 |
|---|---|
| 托管 | GitHub，公开；许可证见 `LICENSE` |
| 合并方式 | 只允许 squash，默认标题取 PR 标题、正文取 PR 描述；合并后自动删除分支 |
| Auto-merge | 开启（`scripts/pr.sh` 用 `gh pr merge --auto`） |
| Actions 令牌权限 | 默认只读；发布用的 workflow 自己声明 `contents: write`（推 GHCR 另加 `packages: write`） |
| 私密漏洞报告 | 开启（`SECURITY.md` 指向它）：`gh api -X PUT repos/{owner}/{repo}/private-vulnerability-reporting` |
| 提交邮箱 | 用 GitHub 的 noreply 地址（本仓 `git config user.email`），账号设置里勾选「Keep my email addresses private」 |

合并选项（一次设好）：

```bash
gh api -X PATCH repos/{owner}/{repo} -F allow_squash_merge=true -F allow_merge_commit=false -F allow_rebase_merge=false \
  -F allow_auto_merge=true -F delete_branch_on_merge=true -f squash_merge_commit_title=PR_TITLE -f squash_merge_commit_message=PR_BODY
```

`main` 的规则集（`gate` 是 `ci.yml` 里唯一 job 的检查名）：

```bash
gh api repos/{owner}/{repo}/rulesets -X POST --input - <<'JSON'
{"name":"main","target":"branch","enforcement":"active",
 "conditions":{"ref_name":{"include":["~DEFAULT_BRANCH"],"exclude":[]}},
 "rules":[{"type":"deletion"},{"type":"non_fast_forward"},
  {"type":"pull_request","parameters":{"required_approving_review_count":0,"dismiss_stale_reviews_on_push":false,
   "require_code_owner_review":false,"require_last_push_approval":false,"required_review_thread_resolution":false,
   "allowed_merge_methods":["squash"]}},
  {"type":"required_status_checks","parameters":{"strict_required_status_checks_policy":true,
   "required_status_checks":[{"context":"gate"}]}}]}
JSON
```

还没有 CI 时先去掉 `required_status_checks` 一条，第一次 PR 跑出检查后再加。私有仓库的规则集需要 GitHub Pro 才生效；不生效时 `scripts/pr.sh` 会检测到没有必需检查，不预约自动合并。

受保护标签：规则集 `release-tags`，匹配 `refs/tags/v*`，禁止创建、更新、删除和强推，仓库管理员可以绕过，也就是只有维护者能打版本 tag：

```bash
gh api repos/{owner}/{repo}/rulesets -X POST --input - <<'JSON'
{"name":"release-tags","target":"tag","enforcement":"active",
 "conditions":{"ref_name":{"include":["refs/tags/v*"],"exclude":[]}},
 "bypass_actors":[{"actor_id":5,"actor_type":"RepositoryRole","bypass_mode":"always"}],
 "rules":[{"type":"creation"},{"type":"update"},{"type":"deletion"},{"type":"non_fast_forward"}]}
JSON
```

## Secrets 与 Variables

| 名字 | 级别 | 用途 | 使用位置 |
|---|---|---|---|
| — | — | 目前不需要额外 secret：发布用 `github.token`，推 GHCR 也用它 | — |

<!-- 推 Docker Hub 时加仓库 secret `REGISTRY_USER`、`REGISTRY_TOKEN`，并在 release.yml 里设 `IMAGE` -->

## Runner

| workflow | runs-on | 说明 |
|---|---|---|
| `ci.yml` | `ubuntu-24.04`（GitHub 托管） | 必需检查 `gate` |
| `release.yml` | `ubuntu-24.04`（GitHub 托管） | 推 `v*` tag 时构建镜像推到 GHCR，建 Release |

## 本机凭据

| 来源 | 用途 |
|---|---|
| `gh auth login` 的登录状态 | `scripts/pr.sh` 开 PR、预约合并 |

## 部署位置

维护者自己的服务器上用 Docker 部署，镜像取自 GHCR 的 Release。具体主机和编排记录在维护者自己的运维仓，不写进本仓。

## 重建步骤

1. 在 GitHub 新建公开仓，推送代码。
2. 按上文设置合并选项、`main` 规则集和 `release-tags` 规则集。
3. 克隆后运行 `scripts/bootstrap.sh`，再跑 `scripts/check.sh`。

## 演练记录

| 日期 | 内容 | 结果 |
|---|---|---|
