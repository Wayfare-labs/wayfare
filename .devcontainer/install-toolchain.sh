#!/usr/bin/env bash
# Installs the toolchain the devcontainer needs, reading every version from
# the place CI already defines it rather than restating it here (#664, #467):
#
#   Go             env.GO_VERSION in .github/workflows/ci.yml
#   golangci-lint  the golangci-lint-action `version:` in the same file
#   Node           env.NODE_VERSION in .github/workflows/qa-accessibility.yml
#
# Playwright and its browsers are not versioned here at all: they come from
# docs/qa/browser/package-lock.json through `npm ci`.
#
# A version that cannot be read is an error, never a fallback. A container
# that quietly installed some other Go would be exactly the drift #467 was
# filed to stop.
#
# Usage: install-toolchain.sh <dir holding ci.yml and qa-accessibility.yml> [prefix]
set -euo pipefail

src=${1:?usage: install-toolchain.sh <workflow dir> [prefix]}
prefix=${2:-/usr/local}
ci="$src/ci.yml"
qa="$src/qa-accessibility.yml"

# workflow_env FILE KEY prints the quoted value of a top-level `env:` entry.
workflow_env() {
	sed -n -E "s/^  $2: \"([^\"]+)\"[[:space:]]*$/\1/p" "$1" | head -n 1
}

go_version=$(workflow_env "$ci" GO_VERSION)
node_major=$(workflow_env "$qa" NODE_VERSION)
lint_version=$(sed -n -E '/golangci-lint-action@/,/version:/ s/^[[:space:]]+version:[[:space:]]*(v[0-9.]+)[[:space:]]*$/\1/p' "$ci" | head -n 1)

for pair in "GO_VERSION=$go_version" "NODE_VERSION=$node_major" "golangci-lint version=$lint_version"; do
	if [ -z "${pair#*=}" ]; then
		echo "install-toolchain: could not read ${pair%%=*} from the workflows in $src" >&2
		exit 1
	fi
done

case "$(uname -m)" in
x86_64) go_arch=amd64 node_arch=x64 ;;
aarch64 | arm64) go_arch=arm64 node_arch=arm64 ;;
*)
	echo "install-toolchain: unsupported architecture $(uname -m)" >&2
	exit 1
	;;
esac

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# Go, verified against the checksum the Go project publishes beside the archive.
go_tar="go${go_version}.linux-${go_arch}.tar.gz"
curl -fsSL -o "$tmp/$go_tar" "https://dl.google.com/go/$go_tar"
echo "$(curl -fsSL "https://dl.google.com/go/$go_tar.sha256")  $tmp/$go_tar" | sha256sum -c -
rm -rf "$prefix/go"
mkdir -p "$prefix/bin"
tar -C "$prefix" -xzf "$tmp/$go_tar"

# Node: the newest release on the major the QA workflow runs, verified against
# the release's SHASUMS256.txt.
shasums=$(curl -fsSL "https://nodejs.org/dist/latest-v${node_major}.x/SHASUMS256.txt")
node_line=$(printf '%s\n' "$shasums" | grep -E "  node-v${node_major}\.[0-9]+\.[0-9]+-linux-${node_arch}\.tar\.xz$")
node_tar=${node_line##* }
curl -fsSL -o "$tmp/$node_tar" "https://nodejs.org/dist/latest-v${node_major}.x/$node_tar"
(cd "$tmp" && printf '%s\n' "$node_line" | sha256sum -c -)
tar -C "$prefix" --strip-components=1 -xJf "$tmp/$node_tar" \
	--wildcards '*/bin' '*/lib' '*/include' '*/share'

# golangci-lint built from source with the toolchain above, which is what CI's
# `install-mode: goinstall` does: a prebuilt binary is compiled with an older
# Go and refuses this module's target.
GOTOOLCHAIN=local GOFLAGS=-modcacherw GOBIN="$prefix/bin" GOPATH="$tmp/gopath" GOCACHE="$tmp/gocache" \
	"$prefix/go/bin/go" install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${lint_version}"

echo "install-toolchain: go ${go_version}, node $("$prefix/bin/node" --version), golangci-lint ${lint_version}"
