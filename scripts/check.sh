#!/usr/bin/env bash
# 仓库唯一门禁入口：本地、钩子、CI、发布脚本都调用它，各处行为一致。
# 用法：scripts/check.sh [--quick|--full]   --quick 只跑通用检查与 lint；默认 --full
# 每一段都是「step 名称」加命令，任何一步失败立即退出；CI 专有的差异只能是环境变量。
set -euo pipefail
cd "$(dirname "$0")/.."
mode=${1:---full}
case "$mode" in
  --quick|--full) ;;
  *) echo "用法：scripts/check.sh [--quick|--full]" >&2; exit 2 ;;
esac

step() { printf '\n==> %s\n' "$*"; }
need() { command -v "$1" >/dev/null 2>&1 || { echo "缺少 $1，安装：$2" >&2; exit 1; }; }
# 把 git 跟踪的、匹配给定 pathspec 的文件读进数组 files
tracked() {
  files=()
  while IFS= read -r -d '' f; do files+=("$f"); done < <(git ls-files -z -- "$@")
}

# ---- 通用检查（所有仓都跑） ----
step 注释中的流程编号
scripts/lint/comments.sh

step shellcheck
need shellcheck "brew install shellcheck / apt-get install shellcheck"
tracked '*.sh' '.githooks/*' ':!:*.conf'
[ "${#files[@]}" -eq 0 ] || shellcheck "${files[@]}"

# Gitea 仓的 workflow 在 .gitea/，GitHub 仓的在 .github/；actionlint 只自动发现后者，所以两边都显式传文件和配置
for dir in .gitea .github; do
  tracked "$dir/workflows/*.yml" "$dir/workflows/*.yaml"
  [ "${#files[@]}" -gt 0 ] || continue
  step "actionlint（${dir}）"
  need actionlint "brew install actionlint"
  actionlint -config-file "$dir/actionlint.yaml" "${files[@]}"
done

# >>> 按栈的快速检查：bootstrap --stack 在这里插入，也可以手写
# ---- go ----
step gofmt
unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then
  printf '%s\n' "$unformatted"
  echo "以上文件需要 gofmt -w" >&2
  exit 1
fi
step "go vet"
go vet ./...
# ---- node ----
# 前端在 web/：有 pnpm-lock.yaml 用 pnpm，否则用 npm；typecheck、lint、format:check 三个脚本在 web/package.json 里定义
if [ -f web/package.json ]; then
  (
    cd web
    step 按锁文件安装依赖
    if [ -f pnpm-lock.yaml ]; then
      pm=pnpm
      pnpm install --frozen-lockfile
    else
      pm=npm
      npm ci
    fi
    step 类型检查
    "$pm" run --if-present typecheck
    step lint
    "$pm" run --if-present lint
    step 格式
    "$pm" run --if-present format:check
  )
fi
# ---- docker ----
# hadolint 是可选工具：装了才检查 Dockerfile
if command -v hadolint >/dev/null 2>&1; then
  step hadolint
  tracked 'Dockerfile' '*/Dockerfile' '*.Dockerfile'
  [ "${#files[@]}" -eq 0 ] || hadolint "${files[@]}"
fi
# <<< 按栈的快速检查

if [ "$mode" = --quick ]; then
  printf '\n快速检查通过\n'
  exit 0
fi

# >>> 按栈的完整检查：bootstrap --stack 在这里插入，也可以手写
step "go mod tidy"
go mod tidy -diff
step "go build"
go build ./...
step "go test"
go test -race ./...
# 手机端通用脚本：用 Node 自带的测试框架模拟各客户端环境，不需要安装依赖
if [ -f script/ferry.test.js ]; then
  step 手机端脚本测试
  need node "brew install node"
  node --test script/ferry.test.js
fi
# golangci-lint 是可选工具：装了才检查（CI 镜像里没有）
if command -v golangci-lint >/dev/null 2>&1; then
  step golangci-lint
  golangci-lint run ./...
fi
if [ -f web/package.json ]; then
  (
    cd web
    if [ -f pnpm-lock.yaml ]; then pm=pnpm; else pm=npm; fi
    step 前端测试
    "$pm" run --if-present test
    step 前端构建
    "$pm" run build
  )
fi
# <<< 按栈的完整检查

printf '\n完整检查通过\n'
