#!/usr/bin/env bash
# 给已推送的多架构镜像补上说明：在镜像索引上加 OCI 注解，GHCR 包页面的介绍、许可证、关联仓库都读它。
# 不重新构建：imagetools create 用同一批各架构镜像生成带注解的新索引，再推回原来的标签。
# 用法：scripts/image-annotate.sh [--dry-run] <tag，如 v0.1.0 或 edge> <镜像名>...
#   镜像名不带标签，如 ghcr.io/lonelyman0108/ferry docker.io/lonelyman0108/ferry；
#   正式版本同时更新 X.Y 与 latest，预发布版本只更新 X.Y.Z，edge 更新 edge 与 EXTRA_TAGS（空格分隔）。
#   可选环境变量：EXTRA_TAGS、REVISION、CREATED。需要事先登录好各镜像仓库。
set -euo pipefail

dry=0
if [ "${1:-}" = --dry-run ]; then
  dry=1
  shift
fi
tag=${1:?用法：scripts/image-annotate.sh [--dry-run] <tag> <镜像名>...}
shift
[ $# -gt 0 ] || { echo "至少给一个镜像名" >&2; exit 2; }

repo=https://github.com/lonelyman0108/ferry
desc="登录态的摆渡人：在本人手机上接住 App 的登录态，校验、留档，再送进青龙等自动化任务。数据在 /data，首次启动看日志里的 setup_token。文档：$repo"

if [ "$tag" = edge ]; then
  version=edge
  src_tag=edge
  tag_names=(edge)
  if [ -n "${EXTRA_TAGS:-}" ]; then
    read -r -a extra_tags <<< "$EXTRA_TAGS"
    tag_names+=("${extra_tags[@]}")
  fi
else
  version=${tag#v}
  src_tag=$version
  tag_names=("$version")
  case "$version" in
    *-*) ;;
    *) tag_names+=("${version%.*}" latest) ;;
  esac
fi

annotations=(
  "org.opencontainers.image.title=Ferry"
  "org.opencontainers.image.description=$desc"
  "org.opencontainers.image.source=$repo"
  "org.opencontainers.image.url=$repo"
  "org.opencontainers.image.documentation=$repo/blob/main/docs/deployment.md"
  "org.opencontainers.image.licenses=Apache-2.0"
  "org.opencontainers.image.version=$version"
)
revision=${REVISION:-}
created=${CREATED:-}
[ -z "$revision" ] || annotations+=("org.opencontainers.image.revision=$revision")
[ -z "$created" ] || annotations+=("org.opencontainers.image.created=$created")

for image in "$@"; do
  tags=()
  for t in "${tag_names[@]}"; do
    tags+=(-t "$image:$t")
  done
  args=(buildx imagetools create "${tags[@]}")
  for kv in "${annotations[@]}"; do
    args+=(--annotation "index:$kv")
  done
  args+=("$image:$src_tag")
  if [ "$dry" -eq 1 ]; then
    echo "[dry-run] docker ${args[*]}"
  else
    docker "${args[@]}"
    echo "已补说明：$image:$src_tag（${tag_names[*]}）"
  fi
done
