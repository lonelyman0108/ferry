#!/usr/bin/env bash
# Edge 渠道发布脚本：由 .github/workflows/edge.yml 调用，也支持本地 --dry-run 验证。
# 子命令：
#   guard                                            判断是否跳过旧提交，输出元数据（GITHUB_OUTPUT 格式）
#   push-images [--dry-run] [--notes <文件>]         构建并推送多架构 edge / sha-<7位> 镜像并补齐 OCI 注解
#   publish-release [--dry-run] [--notes <文件>] <目录>  移动 edge 标签，就地更新预发布 Release 页并覆盖上传产物
set -euo pipefail

usage() {
  awk 'NR > 1 && /^#/ { sub(/^# ?/, ""); print; next } NR > 1 { exit }' "$0" >&2
  exit 2
}

cmd_guard() {
  local event=${EVENT_NAME:-}
  local full_sha short7 short12 build_date skip edge_sha
  full_sha=$(git rev-parse HEAD)
  short7=${full_sha:0:7}
  short12=${full_sha:0:12}
  build_date=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  skip=false

  if [ "$event" = workflow_run ]; then
    git fetch -q origin refs/tags/edge:refs/tags/edge 2>/dev/null || true
    edge_sha=$(git rev-parse -q --verify 'refs/tags/edge^{commit}' 2>/dev/null || true)
    if [ -n "$edge_sha" ] && [ "$edge_sha" != "$full_sha" ] && git merge-base --is-ancestor "$full_sha" "$edge_sha"; then
      echo "edge 标签已指向更新的提交（${edge_sha:0:7}），跳过旧提交 ${short7}" >&2
      skip=true
    fi
  fi

  printf 'skip=%s\n' "$skip"
  printf 'full_sha=%s\n' "$full_sha"
  printf 'short7=%s\n' "$short7"
  printf 'short12=%s\n' "$short12"
  printf 'build_date=%s\n' "$build_date"
}

cmd_push_images() {
  local dry=0 notes=
  while [ $# -gt 0 ]; do
    case "$1" in
      --dry-run) dry=1; shift ;;
      --notes) notes=${2:?--notes 需要文件路径}; shift 2 ;;
      *) usage ;;
    esac
  done

  local platforms=${PLATFORMS:-linux/amd64,linux/arm64}
  local ghcr_image=${GHCR_IMAGE:-ghcr.io/lonelyman0108/ferry}
  local hub_image=${HUB_IMAGE:-docker.io/lonelyman0108/ferry}
  local full_sha=${FULL_SHA:-$(git rev-parse HEAD)}
  local build_date=${BUILD_DATE:-$(cat .build-date 2>/dev/null || date -u +%Y-%m-%dT%H:%M:%SZ)}
  local short7=${full_sha:0:7}
  local sha_tag="sha-$short7"
  local has_hub=0

  if [ -n "${HUB_TOKEN:-}" ]; then
    has_hub=1
  else
    echo "未配置 DOCKERHUB_TOKEN，跳过 Docker Hub" >&2
  fi

  local images=("$ghcr_image")
  local build_args=(
    buildx build
    --platform "$platforms"
    --build-arg VERSION=edge
    --provenance=false
    --sbom=false
    -t "$ghcr_image:edge"
    -t "$ghcr_image:$sha_tag"
  )
  if [ "$has_hub" -eq 1 ]; then
    images+=("$hub_image")
    build_args+=(-t "$hub_image:edge" -t "$hub_image:$sha_tag")
  fi
  if [ "${GITHUB_ACTIONS:-}" = true ]; then
    build_args+=(--cache-from "type=gha,scope=edge" --cache-to "type=gha,mode=max,scope=edge")
  fi
  build_args+=(--push .)

  if [ "$dry" -eq 1 ]; then
    echo "[dry-run] docker login ghcr.io -u <GHCR_USER> --password-stdin"
    if [ "$has_hub" -eq 1 ]; then
      echo "[dry-run] docker login -u <HUB_USER> --password-stdin"
    fi
    echo "[dry-run] docker ${build_args[*]}"
    EXTRA_TAGS="$sha_tag" REVISION="$full_sha" CREATED="$build_date" \
      scripts/image-annotate.sh --dry-run edge "${images[@]}"
  else
    command -v docker >/dev/null 2>&1 || { echo "缺少 docker" >&2; exit 2; }
    if [ -n "${GHCR_TOKEN:-}" ]; then
      local ghcr_user=${GHCR_USER:?有 GHCR_TOKEN 时需要提供 GHCR_USER}
      printf '%s' "$GHCR_TOKEN" | docker login ghcr.io -u "$ghcr_user" --password-stdin
    fi
    if [ "$has_hub" -eq 1 ]; then
      local hub_user=${HUB_USER:?有 HUB_TOKEN 时需要提供 HUB_USER}
      printf '%s' "$HUB_TOKEN" | docker login -u "$hub_user" --password-stdin
    fi
    docker "${build_args[@]}"
    EXTRA_TAGS="$sha_tag" REVISION="$full_sha" CREATED="$build_date" \
      scripts/image-annotate.sh edge "${images[@]}"
  fi

  if [ -n "$notes" ]; then
    {
      echo "### 镜像"
      echo "- \`$ghcr_image:edge\`"
      echo "- \`$ghcr_image:$sha_tag\`"
      if [ "$has_hub" -eq 1 ]; then
        echo
        echo "### 镜像（Docker Hub）"
        echo "- \`$hub_image:edge\`"
        echo "- \`$hub_image:$sha_tag\`"
      fi
    } > "$notes"
  fi
}

cmd_publish_release() {
  local dry=0 notes=
  while [ $# -gt 0 ]; do
    case "$1" in
      --dry-run) dry=1; shift ;;
      --notes) notes=${2:?--notes 需要文件路径}; shift 2 ;;
      -*) usage ;;
      *) break ;;
    esac
  done
  local dist=${1:-}
  if [ "$dry" -eq 0 ] && [ -z "$dist" ]; then
    usage
  fi

  local server=${GITHUB_SERVER_URL:-https://github.com}
  server=${server%/}
  local repo=${GITHUB_REPOSITORY:-lonelyman0108/ferry}
  local full_sha=${FULL_SHA:-$(git rev-parse HEAD)}
  local short7=${full_sha:0:7}
  local short12=${full_sha:0:12}
  local build_date=${BUILD_DATE:-$(cat .build-date 2>/dev/null || date -u +%Y-%m-%dT%H:%M:%SZ)}
  local run_number=${GITHUB_RUN_NUMBER:-}
  local run_id=${GITHUB_RUN_ID:-}

  local title="Edge ($short7)"
  if [ -n "$run_number" ]; then
    title="Edge #${run_number} ($short7)"
  fi

  local last_stable
  last_stable=$(git tag -l 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -n 1 || true)

  local notes_file
  notes_file=$(mktemp)
  trap 'rm -f "$notes_file"' RETURN

  {
    echo "> 跟随 \`main\` 最新通过 CI 的提交自动构建的预发布版本，未经正式发版验证。生产环境建议使用最新正式版或固定 \`sha-<提交>\` 标签。"
    echo
    echo "- 版本：\`edge\`"
    echo "- 提交：[\`$short12\`]($server/$repo/commit/$full_sha)"
    if [ -n "$run_number" ] && [ -n "$run_id" ]; then
      echo "- 构建：[#$run_number]($server/$repo/actions/runs/$run_id)"
    fi
    echo "- 构建时间：\`$build_date\`"
    if [ -n "$last_stable" ]; then
      local count
      count=$(git rev-list --count "$last_stable..$full_sha" 2>/dev/null || echo 0)
      echo
      echo "### 自 $last_stable 以来的变更"
      echo
      if [ "$count" -gt 0 ]; then
        git log -n 50 --format="- \`%h\` %s" "$last_stable..$full_sha"
        if [ "$count" -gt 50 ]; then
          echo
          echo "其余 $((count - 50)) 条提交见完整对比。"
        fi
        echo
        echo "[查看完整对比]($server/$repo/compare/$last_stable...$short12)"
      else
        echo "与 \`$last_stable\` 代码一致。"
      fi
    fi
    if [ -n "$notes" ]; then
      [ -f "$notes" ] || { echo "找不到 $notes" >&2; exit 1; }
      echo
      cat "$notes"
    fi
  } > "$notes_file"

  if [ "$dry" -eq 1 ]; then
    printf '标签：edge（预发布：true）\n标题：%s\n----\n' "$title"
    cat "$notes_file"
    return 0
  fi

  command -v gh >/dev/null 2>&1 || { echo "缺少 gh" >&2; exit 2; }
  [ -n "${GH_TOKEN:-}" ] || { echo "缺少 GH_TOKEN" >&2; exit 2; }

  shopt -s nullglob
  local assets=("$dist"/ferry_edge_*.tar.gz "$dist"/SHA256SUMS)
  shopt -u nullglob
  [ "${#assets[@]}" -gt 0 ] || { echo "$dist 里没有可上传的产物" >&2; exit 1; }

  if gh api "repos/$repo/git/refs/tags/edge" >/dev/null 2>&1; then
    gh api -X PATCH "repos/$repo/git/refs/tags/edge" -f "sha=$full_sha" -F force=true >/dev/null
  else
    gh api -X POST "repos/$repo/git/refs" -f ref=refs/tags/edge -f "sha=$full_sha" >/dev/null
  fi

  # 编辑已有预发布而不是删了重建，避免每次合并都给关注者发新的 Release 通知
  if gh release view edge --repo "$repo" >/dev/null 2>&1; then
    gh release edit edge --repo "$repo" --title "$title" --notes-file "$notes_file" --prerelease
    echo "已更新 Edge Release（$title）"
  else
    gh release create edge --repo "$repo" --verify-tag --title "$title" --notes-file "$notes_file" --prerelease
    echo "已创建 Edge Release（$title）"
  fi
  gh release upload edge --repo "$repo" "${assets[@]}" --clobber
}

sub=${1:-}
[ -n "$sub" ] || usage
shift
case "$sub" in
  guard) cmd_guard "$@" ;;
  push-images) cmd_push_images "$@" ;;
  publish-release) cmd_publish_release "$@" ;;
  *) usage ;;
esac
