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
case "$1 $2" in
  "rev-parse HEAD") printf '%040d\n' 0 ;;
  "tag --merged") exit 0 ;;
  "ls-remote --exit-code") exit 2 ;;
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

if ! (
  PATH="$fake_bin:$PATH"
  BUILDKITE_COMMIT=0000000000000000000000000000000000000000
  BUILDKITE_AGENT_CALLS="$fake_bin/buildkite-agent-calls"
  GITHUB_TOKEN=test-token
  export PATH BUILDKITE_COMMIT BUILDKITE_AGENT_CALLS GITHUB_TOKEN
  main >/dev/null
); then
  fail 'tag command fails after pushing the tag'
fi
assert_contains "$fake_bin/buildkite-agent-calls" 'meta-data set release-tag v0.0.1'

assert_equal 1 "$(grep -Fc 'if: build.branch != "main"' "$root/.buildkite/pipeline.yml")"
assert_contains "$root/.buildkite/pipeline.release.yml" 'input: ":package: Release"'
assert_contains "$root/.buildkite/pipeline.release.yml" 'key: "release"'
assert_contains "$root/.buildkite/pipeline.release.yml" 'blocked_state: "passed"'
assert_contains "$root/.buildkite/pipeline.release.yml" 'value: "major"'
assert_contains "$root/.buildkite/pipeline.release.yml" 'command: ".buildkite/tag.sh"'
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
