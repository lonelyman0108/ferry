#!/usr/bin/env bash
# 给已推送的多架构镜像补上说明：在镜像索引上加 OCI 注解，GHCR 包页面的介绍、许可证、关联仓库都读它。
# 不重新构建：imagetools create 用同一批各架构镜像生成带注解的新索引，再推回原来的标签。
# 用法：scripts/image-annotate.sh <tag，如 v0.1.0> <镜像名>...
#   镜像名不带标签，如 ghcr.io/lonelyman0108/ferry docker.io/lonelyman0108/ferry；正式版本同时更新 X.Y 与 latest，
#   预发布版本只更新 X.Y.Z。需要事先登录好各镜像仓库。
set -euo pipefail
tag=${1:?用法：scripts/image-annotate.sh <tag> <镜像名>...}
shift
[ $# -gt 0 ] || { echo "至少给一个镜像名" >&2; exit 2; }
version=${tag#v}
repo=https://github.com/lonelyman0108/ferry
desc="登录态的摆渡人：在本人手机上接住 App 的登录态，校验、留档，再送进青龙等自动化任务。数据在 /data，首次启动看日志里的 setup_token。文档：$repo"

for image in "$@"; do
  tags=(-t "$image:$version")
  case "$version" in
    *-*) ;;
    *) tags+=(-t "$image:${version%.*}" -t "$image:latest") ;;
  esac
  args=(buildx imagetools create "${tags[@]}")
  for kv in \
    "org.opencontainers.image.title=Ferry" \
    "org.opencontainers.image.description=$desc" \
    "org.opencontainers.image.source=$repo" \
    "org.opencontainers.image.url=$repo" \
    "org.opencontainers.image.documentation=$repo/blob/main/docs/deployment.md" \
    "org.opencontainers.image.licenses=Apache-2.0" \
    "org.opencontainers.image.version=$version"; do
    args+=(--annotation "index:$kv")
  done
  args+=("$image:$version")
  docker "${args[@]}"
  echo "已补说明：$image:$version"
done
