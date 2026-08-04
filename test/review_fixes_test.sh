#!/bin/bash

set -u

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
FAILURES=0

run_test() {
  local name=$1

  if "$name"; then
    printf 'ok - %s\n' "$name"
  else
    printf 'not ok - %s\n' "$name"
    FAILURES=$((FAILURES + 1))
  fi
}

assert_requires_organization_before_api_call() {
  local script=$1
  local command=${2:-}
  local temporary_directory fake_bin curl_marker output status api_called
  local -a script_arguments=()

  [[ -n "$command" ]] && script_arguments+=("$command")

  temporary_directory=$(mktemp -d)
  fake_bin="$temporary_directory/bin"
  curl_marker="$temporary_directory/curl-called"
  mkdir -p "$fake_bin"
  cp "$ROOT/$script" "$temporary_directory/$script"
  cp -R "$ROOT/util" "$temporary_directory/util"
  cat > "$fake_bin/curl" <<'EOF'
#!/bin/bash
touch "$CURL_MARKER"
exit 99
EOF
  chmod +x "$fake_bin/curl"

  output=$(env -i \
    PATH="$fake_bin:$PATH" \
    HOME="${HOME:-}" \
    CURL_MARKER="$curl_marker" \
    BUILDKITE_API_TOKEN=test-token \
    BUILDKITE_API_URL=http://127.0.0.1:1 \
    "$temporary_directory/$script" "${script_arguments[@]}" 2>&1)
  status=$?
  api_called=false
  [[ -e "$curl_marker" ]] && api_called=true

  rm -rf "$temporary_directory"

  [[ $status -ne 0 ]] &&
    [[ "$output" == *BUILDKITE_ORGANIZATION_SLUG* ]] &&
    [[ "$api_called" == false ]]
}

test_setup_requires_organization_before_api_call() {
  assert_requires_organization_before_api_call setup-organization.sh
}

test_workload_requires_organization_before_api_call() {
  assert_requires_organization_before_api_call workload-generator.sh run
}

test_new_pipeline_uses_requested_branch() {
  local payload_file

  payload_file=$(mktemp)
  (
    COMMON_CURL_ARGS=()
    BUILDKITE_API_URL=https://api.example.test/v2
    BUILDKITE_ORGANIZATION_SLUG=test-org
    source "$ROOT/util/setup-organization/functions.sh"

    curl() {
      cat > "$payload_file"
      printf '{}\n'
    }

    create_demo_pipeline \
      test-pipeline \
      "Test Pipeline" \
      test-queue \
      pipeline.yml \
      team-id \
      main >/dev/null
  )
  jq -e '.default_branch == "main"' "$payload_file" >/dev/null
  local status=$?
  rm -f "$payload_file"
  return "$status"
}

test_reused_pipeline_rejects_mismatched_branch() {
  (
    source "$ROOT/util/setup-organization/functions.sh"

    pipeline_by_slug() {
      printf '{"slug":"test-pipeline","default_branch":"stale-branch"}\n'
    }

    create_demo_pipeline() {
      return 99
    }

    ! ensure_demo_pipeline \
      test-pipeline \
      "Test Pipeline" \
      test-queue \
      pipeline.yml \
      team-id \
      main >/dev/null 2>&1
  )
}

run_test test_setup_requires_organization_before_api_call
run_test test_workload_requires_organization_before_api_call
run_test test_new_pipeline_uses_requested_branch
run_test test_reused_pipeline_rejects_mismatched_branch

exit "$FAILURES"
