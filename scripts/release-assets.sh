#!/bin/sh
# Generates the shell completions and the man page that every release package
# ships, into completions/ and manpages/ at the repository root. GoReleaser runs
# it before building (see .goreleaser.yaml); both folders are gitignored.
#
# The man page carries the date SOURCE_DATE_EPOCH names. Unless the packager
# set one, it is the commit's, so two builds of one commit ship the same page.
set -eu

cd "$(dirname "$0")/.."

: "${SOURCE_DATE_EPOCH:=$(git log -1 --format=%ct 2>/dev/null || true)}"
export SOURCE_DATE_EPOCH

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
go build -o "$tmp/study" ./cmd/study

rm -rf completions manpages
mkdir -p completions manpages
for shell in bash zsh fish; do
	"$tmp/study" completion "$shell" >"completions/study.$shell"
done
# Not piped into gzip: a failing study man would go unnoticed, and an empty
# page, once compressed, is not an empty file.
"$tmp/study" man >"$tmp/study.1"
gzip -9n <"$tmp/study.1" >manpages/study.1.gz

for f in completions/study.bash completions/study.zsh completions/study.fish manpages/study.1.gz; do
	if [ ! -s "$f" ]; then
		echo "release-assets: $f is empty" >&2
		exit 1
	fi
done
