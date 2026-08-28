#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

fail() { printf 'release-test: %s\n' "$*" >&2; exit 1; }
assert_equal() {
  [[ "$1" == "$2" ]] || fail "expected '$1', got '$2'"
}
assert_contains() {
  grep -Fq -- "$2" "$1" || fail "$1 does not contain '$2'"
}

# shellcheck source=tag.sh
source "$root/.buildkite/tag.sh"

assert_equal v1.2.4 "$(next_release_version v1.2.3 patch)"
assert_equal v1.3.0 "$(next_release_version v1.2.3 minor)"
assert_equal v2.0.0 "$(next_release_version v1.2.3 major)"

fake_bin="$(mktemp -d)"
trap 'rm -rf "$fake_bin"' EXIT
cat > "$fake_bin/git" <<'EOF'
#!/usr/bin/env bash
case "$*" in
  "rev-parse HEAD") printf '%040d\n' 0 ;;
  "tag --merged "*) exit 0 ;;
  "ls-remote --exit-code origin refs/heads/main")
    printf '%s\trefs/heads/main\n' "${FAKE_REMOTE_MAIN:-0000000000000000000000000000000000000000}"
    ;;
  "ls-remote --exit-code --tags origin refs/tags/"*) exit 2 ;;
esac
EOF
cat > "$fake_bin/buildkite-agent" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$BUILDKITE_AGENT_CALLS"
if [[ "$*" == 'meta-data get release-type' ]]; then
  printf 'patch\n'
fi
EOF
cat > "$fake_bin/curl" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
cat > "$fake_bin/tar" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
cat > "$fake_bin/gh" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$fake_bin"/*

run_tag() {
  env \
    PATH="$fake_bin:$PATH" \
    BUILDKITE_COMMIT=0000000000000000000000000000000000000000 \
    BUILDKITE_AGENT_CALLS="$fake_bin/buildkite-agent-calls" \
    GITHUB_TOKEN=test-token \
    FAKE_REMOTE_MAIN="${1:-0000000000000000000000000000000000000000}" \
    "$root/.buildkite/tag.sh" >/dev/null
}

if ! run_tag; then
  fail 'tag command fails after pushing the tag'
fi
assert_contains "$fake_bin/buildkite-agent-calls" 'meta-data set release-tag v0.0.1'
if run_tag 1111111111111111111111111111111111111111; then
  fail 'tag command succeeds for a stale main build'
fi

assert_equal 1 "$(grep -Fc 'if: build.branch != "main"' "$root/.buildkite/pipeline.yml")"
assert_contains "$root/.buildkite/pipeline.release.yml" 'input: ":package: Release"'
assert_contains "$root/.buildkite/pipeline.release.yml" 'key: "release"'
assert_contains "$root/.buildkite/pipeline.release.yml" 'blocked_state: "passed"'
assert_contains "$root/.buildkite/pipeline.release.yml" 'value: "major"'
assert_contains "$root/.buildkite/pipeline.release.yml" 'command: ".buildkite/tag.sh"'
assert_equal 2 "$(grep -Fc 'concurrency: 1' "$root/.buildkite/pipeline.release.yml")"
assert_equal 2 "$(grep -Fc 'concurrency_group: "cluster-migrator-release-tag"' "$root/.buildkite/pipeline.release.yml")"
assert_contains "$root/.buildkite/pipeline.release.yml" 'command: "mise exec -- scripts/ci-buildkite-release"'
assert_equal 2 "$(grep -Fc 'GITHUB_TOKEN: "CLUSTER_MIGRATOR_GITHUB_TOKEN"' "$root/.buildkite/pipeline.release.yml")"
assert_contains "$root/mise.toml" 'shellcheck -x -P .buildkite'
if grep -Eq 'aws-assume-role-with-web-identity|aws-ssm|AWS_REGION|github-token' "$root/.buildkite/pipeline.release.yml"; then
  fail 'the release pipeline still uses AWS to load the GitHub token'
fi
if grep -Eq 'tag\.sh|ci-buildkite-release' "$root/.buildkite/pipeline.yml"; then
  fail 'the CI pipeline still participates in releases'
fi

printf 'release tests passed\n'
