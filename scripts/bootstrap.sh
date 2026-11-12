#!/usr/bin/env bash
# 每次克隆后运行一次：启用 .githooks、检查工具。
# 新项目首次运行时加 --stack 选技术栈（只做一次，完成后删除 templates/）：
#   scripts/bootstrap.sh --stack go              栈：generic / go / node / java / docker / swift（ios、ios-macos 同 swift）
#   scripts/bootstrap.sh --stack go,node         多个栈用逗号分隔，第一个栈决定 runner label
#   --release / --no-release                     覆盖栈的默认值，决定是否启用 release.yml
#   --nightly                                    同时启用 nightly.yml
# 以后再运行（不带 --stack）只做启用钩子与工具检查。每一步都可重复执行。
set -euo pipefail
cd "$(dirname "$0")/.."

die() { printf '%s\n' "$*" >&2; exit 1; }
stacks=
release=
nightly=0
while [ $# -gt 0 ]; do
  case "$1" in
    --stack) stacks=${2:?--stack 需要参数}; shift 2 ;;
    --release) release=yes; shift ;;
    --no-release) release=no; shift ;;
    --nightly) nightly=1; shift ;;
    -h|--help) awk 'NR > 1 && /^#/ { sub(/^# ?/, ""); print; next } NR > 1 { exit }' "$0"; exit 0 ;;
    *) die "未知参数：$1（--help 查看用法）" ;;
  esac
done

# 在 check.sh 的「# <<< 标记」前插入片段；片段首行（# ---- 栈 ----）已存在时跳过
insert_part() {
  local part=$1 marker=$2 head
  [ -s "$part" ] || return 0
  head=$(sed -n 1p "$part")
  grep -qxF -- "$head" scripts/check.sh && return 0
  awk -v f="$part" -v m="# <<< $marker" '$0 == m { while ((getline l < f) > 0) print l; close(f) } { print }' \
    scripts/check.sh > scripts/check.sh.tmp
  cat scripts/check.sh.tmp > scripts/check.sh
  rm -f scripts/check.sh.tmp
}

# 把工作流模板放到位并替换 runner label（超时沿用模板里的值）
render_workflow() {
  local src=$1 dst=$2
  sed -e "s/__RUNS_ON__/$runs_on/" "$src" > "$dst.tmp"
  mv "$dst.tmp" "$dst"
}

if [ -n "$stacks" ]; then
  [ -d templates/stacks ] || die "templates/ 已不存在：栈初始化只在新项目首次运行时做一次"
  runs_on=
  timeout=
  stack_release=no
  release_workflow=release.yml
  IFS=, read -r -a list <<< "$stacks"
  for s in "${list[@]}"; do
    case "$s" in ios|ios-macos|macos) s=swift ;; esac
    dir=templates/stacks/$s
    [ -d "$dir" ] || die "未知栈：${s}（可选：$(cd templates/stacks && echo *)）"
    RUNS_ON=''
    TIMEOUT=''
    RELEASE=no
    RELEASE_WORKFLOW=
    # shellcheck source=/dev/null
    . "$dir/stack.conf"
    [ -n "$runs_on" ] || { runs_on=$RUNS_ON; timeout=$TIMEOUT; }
    [ "$RELEASE" = yes ] && stack_release=yes
    # 栈自带的发布 workflow（例如 docker 的构建推送）优先于通用的 release.yml
    [ -z "$RELEASE_WORKFLOW" ] || release_workflow=$RELEASE_WORKFLOW
    # 栈附带的脚本（例如 scripts/ci/docker-release.sh）按相对路径放进仓库
    if [ -d "$dir/files" ]; then
      cp -R "$dir/files/." .
    fi
    insert_part "$dir/check-quick.part" 按栈的快速检查
    insert_part "$dir/check-full.part" 按栈的完整检查
    if [ -s "$dir/gitignore.part" ] && ! grep -qxF -- "$(sed -n 1p "$dir/gitignore.part")" .gitignore; then
      { echo; cat "$dir/gitignore.part"; } >> .gitignore
    fi
    echo "已应用栈 $s"
  done
  release=${release:-$stack_release}

  sed -e "s/__RUNS_ON__/$runs_on/" -e "s/^\(    timeout-minutes: \)[0-9][0-9]*/\1$timeout/" \
    .gitea/workflows/ci.yml > .gitea/workflows/ci.yml.tmp
  mv .gitea/workflows/ci.yml.tmp .gitea/workflows/ci.yml
  echo "ci.yml：runs-on ${runs_on}，timeout-minutes $timeout"
  if [ "$release" = yes ]; then
    render_workflow "templates/workflows/$release_workflow" .gitea/workflows/release.yml
    echo "已启用 release.yml（取自 ${release_workflow}）"
  fi
  if [ "$nightly" -eq 1 ]; then
    render_workflow templates/workflows/nightly.yml .gitea/workflows/nightly.yml
    echo "已启用 nightly.yml"
  fi
  # 模板专用文件：生成的新仓不再需要
  rm -rf templates .gitea/template
fi

grep -q '__RUNS_ON__' .gitea/workflows/*.yml 2>/dev/null \
  && echo "提示：ci.yml 的 runs-on 还是占位 __RUNS_ON__，运行 scripts/bootstrap.sh --stack <栈> 或手工改成 runner label" >&2

chmod +x .githooks/commit-msg .githooks/pre-commit
git config core.hooksPath .githooks
echo "core.hooksPath = $(git config core.hooksPath)"

echo "工具检查："
for t in shellcheck actionlint jq curl gitleaks git-cliff; do
  case "$t" in
    shellcheck|actionlint) level=必需 ;;
    jq|curl) level='pr.sh 与发布' ;;
    *) level=可选 ;;
  esac
  if command -v "$t" >/dev/null 2>&1; then
    printf '  有  %-11s（%s）\n' "$t" "$level"
  else
    printf '  缺  %-11s（%s，brew install %s）\n' "$t" "$level" "$t"
  fi
done
echo "初始化完成"
