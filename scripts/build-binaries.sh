#!/usr/bin/env bash
# 发布用的二进制：linux、darwin 各 amd64、arm64，内嵌管理界面，打成 tar.gz 并附 SHA256SUMS。
# 用法：scripts/build-binaries.sh <版本号，如 0.1.0> [输出目录，默认 dist]
# 需要先构建好 web/dist。提交号与构建时间取当前 git 状态。
set -euo pipefail
version=${1:?用法：scripts/build-binaries.sh <版本号> [输出目录]}
out=${2:-dist}
[ -f web/dist/index.html ] || { echo "缺少 web/dist：先在 web/ 下 npm run build" >&2; exit 2; }
commit=$(git rev-parse --short=12 HEAD)
date=$(date -u +%Y-%m-%dT%H:%M:%SZ)
pkg=github.com/lonelyman0108/ferry/internal/buildinfo
mkdir -p "$out"
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  os=${target%/*}
  arch=${target#*/}
  name="ferry_${version}_${os}_${arch}"
  dir=$(mktemp -d)
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -tags embed_ui \
    -ldflags "-s -w -X $pkg.Version=$version -X $pkg.Commit=$commit -X $pkg.Date=$date" \
    -o "$dir/ferry" ./cmd/ferry
  cp LICENSE NOTICE README.md "$dir/"
  tar -C "$dir" -czf "$out/$name.tar.gz" ferry LICENSE NOTICE README.md
  rm -rf "$dir"
  echo "已构建 $out/$name.tar.gz"
done
(cd "$out" && shasum -a 256 ferry_*.tar.gz > SHA256SUMS)
