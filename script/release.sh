#!/bin/sh
# Build everything a release ships: the lm compiler for each platform, the VS Code extension, and a
# checksum file. Writes dist/ and nothing else — publishing is a separate step (see release.yml,
# or run `gh release create` by hand), so this script is safe to run any time.
#
# Usage: script/release.sh v0.7.1
#
# Why per-platform binaries at all: `go install` needs Go and a compile, and a project that only
# wants to build with Loom should not need a toolchain. A download that is one tarball keeps the
# install of a build tool out of the way of the build.
set -eu

version=${1:-}
case "$version" in
  v[0-9]*) ;;
  *) echo "usage: script/release.sh v<major>.<minor>.<patch>   (e.g. v0.7.1)" >&2; exit 2 ;;
esac

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
dist="$root/dist"

# The extension carries its own version; a release where the two disagree ships an extension that
# says it is something else.
ext=$(sed -n 's/^  "version": "\(.*\)",$/\1/p' editors/vscode/package.json | head -1)
if [ "v$ext" != "$version" ]; then
  echo "⛔ editors/vscode/package.json says $ext, this release is $version — bump it first" >&2
  exit 1
fi

# The Windows archive needs zip, and finding that out halfway through leaves a half-built dist/.
command -v zip >/dev/null 2>&1 || { echo "⛔ zip is needed for the Windows archive: install it and rerun" >&2; exit 1; }

# A release built from a dirty tree is not the tag it claims to be. In CI the tree is a fresh
# checkout; locally this is the one thing worth saying out loud.
if command -v git >/dev/null 2>&1 && [ -n "$(git status --porcelain 2>/dev/null)" ]; then
  echo "⚠ the working tree has uncommitted changes — this build is not exactly $version" >&2
fi

rm -rf "$dist"
mkdir -p "$dist"

echo "── lm $version ──"
# arm64 mac, intel mac, the two common Linux architectures, and Windows on amd64.
for target in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64; do
  os=${target%/*}
  arch=${target#*/}
  name="lm_${version}_${os}_${arch}"
  work="$dist/$name"
  bin=lm
  [ "$os" = windows ] && bin=lm.exe
  mkdir -p "$work"
  # -trimpath: the archive should not carry this machine's paths. -s -w: no debug tables, since a
  # stack trace from a released binary is read by its version, not by local symbols.
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -ldflags "-s -w -X main.version=$version" -o "$work/$bin" ./cmd/lm
  cp LICENSE README.md "$work/"
  if [ "$os" = windows ]; then
    ( cd "$dist" && zip -q -r "$name.zip" "$name" )
  else
    ( cd "$dist" && tar -czf "$name.tar.gz" "$name" )
  fi
  rm -rf "$work"
  printf '  %s\n' "$name"
done

echo "── VS Code extension ──"
make -s vs
vsix="editors/vscode/loom-lang-$ext.vsix"
[ -f "$vsix" ] || { echo "⛔ $vsix was not produced" >&2; exit 1; }
cp "$vsix" "$dist/"
printf '  %s\n' "$(basename "$vsix")"

echo "── checksums ──"
# The list is taken before the redirect creates the file, or SHA256SUMS would checksum itself.
( cd "$dist" && set --; for f in *; do [ "$f" = SHA256SUMS ] || set -- "$@" "$f"; done
  if command -v shasum >/dev/null 2>&1; then shasum -a 256 "$@"; else sha256sum "$@"; fi > SHA256SUMS )
sed 's/^/  /' "$dist/SHA256SUMS"

echo
echo "dist/ is ready. To publish (this is the outward-facing step):"
echo "  gh release create $version dist/* --generate-notes"
