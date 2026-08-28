#!/usr/bin/env bash
set -euo pipefail

die() { printf '[tag] error: %s\n' "$*" >&2; exit 1; }
temporary_directory=''
cleanup_temporary_directory() { rm -rf "$temporary_directory"; }

next_release_version() {
  local current=$1 release_type=$2 major minor patch

  [[ "$current" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] \
    || return 1
  major=${BASH_REMATCH[1]}
  minor=${BASH_REMATCH[2]}
  patch=${BASH_REMATCH[3]}

  case "$release_type" in
    patch) printf 'v%d.%d.%d\n' "$major" "$minor" "$((10#$patch + 1))" ;;
    minor) printf 'v%d.%d.0\n' "$major" "$((10#$minor + 1))" ;;
    major) printf 'v%d.0.0\n' "$((10#$major + 1))" ;;
    *) return 1 ;;
  esac
}

latest_release_version() {
  local commit=$1 tag

  while IFS= read -r tag; do
    if [[ "$tag" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
      printf '%s\n' "$tag"
      return
    fi
  done < <(git tag --merged "$commit" --list 'v*' --sort=-version:refname)

  printf 'v0.0.0\n'
}

main() {
  local release_type latest tag gh_version gh_directory remote_main

  : "${BUILDKITE_COMMIT:?BUILDKITE_COMMIT is required}"
  : "${GITHUB_TOKEN:?GITHUB_TOKEN is required}"
  [[ "$BUILDKITE_COMMIT" =~ ^[0-9a-f]{40}$ ]] \
    || die 'BUILDKITE_COMMIT must be a full lowercase commit ID'
  [[ "$(git rev-parse HEAD)" == "$BUILDKITE_COMMIT" ]] \
    || die 'checked-out HEAD does not match BUILDKITE_COMMIT'

  release_type="$(buildkite-agent meta-data get release-type)"
  git fetch --force --tags origin
  latest="$(latest_release_version "$BUILDKITE_COMMIT")"
  tag="$(next_release_version "$latest" "$release_type")" \
    || die "unsupported release type: $release_type"

  if [[ "$latest" != v0.0.0 ]] \
    && [[ "$(git rev-list -n 1 "$latest^{commit}")" == "$BUILDKITE_COMMIT" ]]; then
    die "no commits since $latest"
  fi
  git ls-remote --exit-code --tags origin "refs/tags/$tag" >/dev/null 2>&1 \
    && die "tag $tag already exists at origin"

  gh_version=2.57.0
  gh_directory="gh_${gh_version}_linux_amd64"
  temporary_directory="$(mktemp -d)"
  trap cleanup_temporary_directory EXIT
  curl -fsSL "https://github.com/cli/cli/releases/download/v${gh_version}/${gh_directory}.tar.gz" \
    | tar -xz -C "$temporary_directory"
  export PATH="$temporary_directory/$gh_directory/bin:$PATH"

  gh auth setup-git
  remote_main="$(git ls-remote --exit-code origin refs/heads/main)" \
    || die 'failed to resolve origin main'
  remote_main=${remote_main%%[[:space:]]*}
  [[ "$remote_main" == "$BUILDKITE_COMMIT" ]] \
    || die "cannot release stale build: origin main is $remote_main"
  git tag "$tag" "$BUILDKITE_COMMIT"
  buildkite-agent meta-data set release-tag "$tag"
  git push origin "$tag"
  printf 'Pushed %s; this build will publish it.\n' "$tag"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
