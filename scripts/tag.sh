#!/usr/bin/env bash
# Create the next semver tag (vX.Y.Z) by bumping the latest one, and push it
# to trigger the release workflow.
#
# Usage: scripts/tag.sh <major|minor|patch> [--dry-run]
# M, m and p are accepted as short forms of major, minor and patch.
set -euo pipefail

usage() {
	echo "usage: $0 <major|minor|patch|M|m|p> [--dry-run]" >&2
	exit 2
}

bump="" dry_run=false
for arg in "$@"; do
	case $arg in
	major | M) bump=major ;;
	minor | m) bump=minor ;;
	patch | p) bump=patch ;;
	-n | --dry-run) dry_run=true ;;
	*) usage ;;
	esac
done
[[ -n $bump ]] || usage

remote=origin
branch=main

[[ $(git rev-parse --abbrev-ref HEAD) == "$branch" ]] || {
	echo "error: not on $branch" >&2
	exit 1
}
[[ -z $(git status --porcelain) ]] || {
	echo "error: working tree is not clean" >&2
	exit 1
}

git fetch --quiet --tags "$remote" "$branch"
[[ $(git rev-parse HEAD) == $(git rev-parse "$remote/$branch") ]] || {
	echo "error: $branch is not in sync with $remote/$branch" >&2
	exit 1
}

latest=$(git tag --list 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -n1 || true)
IFS=. read -r major minor patch <<<"${latest#v}"
major=${major:-0} minor=${minor:-0} patch=${patch:-0}

case $bump in
major) next="v$((major + 1)).0.0" ;;
minor) next="v$major.$((minor + 1)).0" ;;
patch) next="v$major.$minor.$((patch + 1))" ;;
esac

if [[ $(git describe --tags --exact-match HEAD 2>/dev/null) == v* ]]; then
	echo "error: HEAD is already tagged $(git describe --tags --exact-match HEAD)" >&2
	exit 1
fi

echo "${latest:-(none)} -> $next"
if $dry_run; then
	exit 0
fi

read -r -p "Tag $(git rev-parse --short HEAD) as $next and push to $remote? [y/N] " answer
[[ $answer == [yY] ]] || exit 1

git tag -a "$next" -m "$next"
git push "$remote" "$next"
