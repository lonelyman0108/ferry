#!/usr/bin/env bash
# 受管文件，来自 ci-workflows@v2.0.0 的 scripts/docker-release.sh：不在本仓直接改，见 CONTRIBUTING.md「受管文件」
# tag 触发的镜像发布：从 vX.Y.Z 推出镜像 tag X.Y.Z 与 X.Y（预发布版本只推 X.Y.Z），用 buildx 构建并推送。
# 部署不在这里做：由维护者手动把 compose 里的镜像版本改成 X.Y.Z 后重建。
#
# 用法：docker-release.sh [--dry-run] [--notes <文件>] <tag>
#   --dry-run      只打印将要执行的登录与构建命令，不调用 docker
#   --notes <文件>  把推送的镜像地址写进该文件，供 release-page.sh --notes 追加到 Release 正文
# 环境变量：
#   IMAGE           镜像名（不含 tag）；默认推平台自带的容器仓库：GitHub 上是 ghcr.io/<owner>/<repo>，
#                   Gitea 上是 <Gitea 主机>/<owner>/<repo>
#   PLATFORMS       buildx 平台，默认 linux/amd64；多架构依赖宿主机已注册 binfmt
#   PUSH_LATEST     设为 yes 时另推 latest（预发布版本不推）；默认不推
#   REGISTRY_USER、REGISTRY_TOKEN  登录镜像仓库：GitHub 用 github.actor 与 github.token（packages: write）；
#                   Gitea 用仓库级 secret，GITEA_TOKEN 不能推包
set -euo pipefail

dry=0
notes=
while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) dry=1; shift ;;
    --notes) notes=${2:?--notes 需要文件路径}; shift 2 ;;
    -*) echo "未知参数：$1" >&2; exit 2 ;;
    *) break ;;
  esac
done
tag=${1:?用法：docker-release.sh [--dry-run] [--notes <文件>] <tag>}
case "$tag" in
  v[0-9]*.[0-9]*.[0-9]*) ;;
  *) echo "tag 应为 vX.Y.Z：$tag" >&2; exit 2 ;;
esac
version=${tag#v}

image=${IMAGE:-}
if [ -z "$image" ]; then
  server=${GITHUB_SERVER_URL:?缺少 GITHUB_SERVER_URL，或用 IMAGE 指定镜像名}
  repo=${GITHUB_REPOSITORY:?缺少 GITHUB_REPOSITORY，或用 IMAGE 指定镜像名}
  host=${server#*://}
  host=${host%/}
  [ "$host" != github.com ] || host=ghcr.io
  image="$host/$repo"
fi
# 镜像名只允许小写
image=$(printf '%s' "$image" | tr '[:upper:]' '[:lower:]')
platforms=${PLATFORMS:-linux/amd64}

tags=("$image:$version")
case "$version" in
  *-*) ;;
  *)
    tags+=("$image:${version%.*}")
    [ "${PUSH_LATEST:-}" != yes ] || tags+=("$image:latest")
    ;;
esac

# 第一段含「.」或「:」的是镜像仓库主机，否则是 Docker Hub
registry=${image%%/*}
case "$registry" in *.*|*:*) ;; *) registry= ;; esac

args=(buildx build --platform "$platforms" --build-arg "VERSION=$version")
for t in "${tags[@]}"; do args+=(-t "$t"); done
args+=(--push .)

if [ "$dry" -eq 1 ]; then
  echo "[dry-run] docker login ${registry:-docker.io} -u <REGISTRY_USER> --password-stdin"
  echo "[dry-run] docker ${args[*]}"
else
  command -v docker >/dev/null 2>&1 || { echo "缺少 docker" >&2; exit 2; }
  if [ -n "${REGISTRY_TOKEN:-}" ]; then
    user=${REGISTRY_USER:?有 REGISTRY_TOKEN 时也要给 REGISTRY_USER}
    if [ -n "$registry" ]; then
      printf '%s' "$REGISTRY_TOKEN" | docker login "$registry" -u "$user" --password-stdin
    else
      printf '%s' "$REGISTRY_TOKEN" | docker login -u "$user" --password-stdin
    fi
  else
    echo "提示：没有 REGISTRY_TOKEN，沿用 runner 上已有的登录状态" >&2
  fi
  docker "${args[@]}"
fi

if [ -n "$notes" ]; then
  {
    echo "### 镜像"
    for t in "${tags[@]}"; do echo "- \`$t\`"; done
  } > "$notes"
fi
echo "镜像：${tags[*]}"
