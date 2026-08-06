#!/bin/bash

set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

# shellcheck source=../util/workload-generator/functions.sh
source "$ROOT/util/workload-generator/functions.sh"
# shellcheck source=../util/setup-organization/functions.sh
source "$ROOT/util/setup-organization/functions.sh"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

assert_equal() {
  local expected=$1
  local actual=$2

  [[ "$actual" == "$expected" ]] ||
    fail "expected '$expected', got '$actual'"
}

assert_contains() {
  local haystack=$1
  local needle=$2

  [[ "$haystack" == *"$needle"* ]] ||
    fail "expected output to contain '$needle'"
}

assert_file_contains() {
  local file=$1
  local text=$2

  grep -Fq -- "$text" "$file" ||
    fail "$file does not contain '$text'"
}

assert_equal 6 "${#WORKLOAD_PIPELINES[@]}"
assert_equal 10 "${#WORKLOAD_QUEUE_KEYS[@]}"
assert_equal "demo-target" "$(workload_pipeline_slug demo target)"
assert_equal "demo-shared" "$(workload_queue_key demo shared)"
assert_equal "Cluster Migrator Target [demo]" \
  "$(workload_pipeline_name demo 'Cluster Migrator Target')"

canonical_schedule=$(workload_schedule smoke)
for load in smoke steady pressure sparse; do
  schedule=$(workload_schedule "$load")
  assert_equal "$canonical_schedule" "$schedule"
  for pipeline in target shared-consumer group-peer isolated-multi dynamic-topology control; do
    assert_contains "$schedule" "$pipeline"
  done
done

assert_equal "dynamic-blue" "$(dynamic_queue_variant 1)"
assert_equal "dynamic-green" "$(dynamic_queue_variant 2)"
assert_equal "dynamic-green" "$(dynamic_queue_variant 10)"

ENSURED_QUEUES=()
ensure_cluster_queue() {
  ENSURED_QUEUES+=("$2")
}
setup_workload_queues cluster-id demo
assert_equal 10 "${#ENSURED_QUEUES[@]}"
assert_contains "${ENSURED_QUEUES[*]}" "demo-private-b"
assert_contains "${ENSURED_QUEUES[*]}" "demo-dynamic-rare"

bootstrap_configuration=$(workload_bootstrap_configuration \
  demo-target-only util/workload-generator/.buildkite/target/pipeline.yml demo)
pipeline=$(jq -n \
  --arg configuration "$bootstrap_configuration" \
  '{repository: "https://example.com/repository", cluster_id: null, configuration: $configuration}')
assert_demo_pipeline_configuration \
  "$pipeline" \
  demo-target \
  demo-target-only \
  util/workload-generator/.buildkite/target/pipeline.yml \
  demo \
  https://example.com/repository
if assert_demo_pipeline_configuration \
    "$pipeline" demo-target wrong-queue \
    util/workload-generator/.buildkite/target/pipeline.yml \
    demo https://example.com/repository 2>/dev/null; then
  fail "pipeline reconciliation accepted the wrong bootstrap queue"
fi

ENSURED_PIPELINES=()
ensure_demo_pipeline() {
  ENSURED_PIPELINES+=("$1|$3|$4")
}
setup_workload_pipelines team-id main demo https://example.com/repository
assert_equal 6 "${#ENSURED_PIPELINES[@]}"
assert_contains "${ENSURED_PIPELINES[*]}" "demo-dynamic-topology|demo-dynamic-bootstrap"

WORKLOAD_COMMIT=0123456789abcdef0123456789abcdef01234567
WORKLOAD_RUN_ID=test-run
WORKLOAD_RESOURCE_PREFIX=demo
WORKLOAD_LOAD=smoke
WORKLOAD_QUICK_SECONDS=1
WORKLOAD_MEDIUM_SECONDS=2
WORKLOAD_LONG_SECONDS=3

payload=$(build_payload main 10 dynamic-rare dynamic-blue)
assert_equal "$WORKLOAD_COMMIT" "$(jq -r '.commit' <<< "$payload")"
assert_equal "main" "$(jq -r '.branch' <<< "$payload")"
assert_equal "test-run" "$(jq -r '.env.MIGRATION_WORKLOAD_RUN_ID' <<< "$payload")"
assert_equal "demo" "$(jq -r '.env.MIGRATION_WORKLOAD_PREFIX' <<< "$payload")"
assert_equal "dynamic-rare" "$(jq -r '.env.MIGRATION_DYNAMIC_QUEUE' <<< "$payload")"
assert_equal "dynamic-blue" "$(jq -r '.env.MIGRATION_DELAYED_QUEUE' <<< "$payload")"

TARGET="$ROOT/util/workload-generator/.buildkite/target/pipeline.yml"
SHARED="$ROOT/util/workload-generator/.buildkite/shared-consumer/pipeline.yml"
PEER="$ROOT/util/workload-generator/.buildkite/group-peer/pipeline.yml"
ISOLATED="$ROOT/util/workload-generator/.buildkite/isolated-multi/pipeline.yml"
DYNAMIC="$ROOT/util/workload-generator/.buildkite/dynamic-topology/pipeline.yml"

assert_file_contains "$TARGET" '${MIGRATION_WORKLOAD_PREFIX}/local/target'
assert_file_contains "$TARGET" '${MIGRATION_WORKLOAD_PREFIX}/shared/cross-pipeline'
assert_file_contains "$SHARED" '${MIGRATION_WORKLOAD_PREFIX}/shared/cross-pipeline'
assert_file_contains "$PEER" '${MIGRATION_WORKLOAD_PREFIX}/shared/cross-pipeline'
assert_file_contains "$ISOLATED" 'parallelism: 2'
assert_file_contains "$DYNAMIC" '${MIGRATION_WORKLOAD_PREFIX}/dynamic/build/${BUILDKITE_BUILD_ID}'
assert_file_contains "$DYNAMIC" '${MIGRATION_WORKLOAD_PREFIX}/dynamic/branch/${BUILDKITE_BRANCH}'
assert_file_contains "$DYNAMIC" 'buildkite-agent pipeline upload'
assert_file_contains "$TARGET" 'buildkite-agent artifact upload'
assert_file_contains "$TARGET" 'buildkite-agent meta-data set'
assert_file_contains "$TARGET" 'buildkite-agent annotate'

generator_help=$("$ROOT/workload-generator.sh" --help)
assert_contains "$generator_help" '--load=<smoke|steady|pressure|sparse>'
assert_contains "$generator_help" '--cycles=<count>'
assert_contains "$generator_help" '--continuous'
assert_contains "$generator_help" 'trigger --workload=late-queue'
if grep -Fq 'commit: "HEAD"' "$ROOT/util/workload-generator/functions.sh"; then
  fail "workload builds must pin an immutable commit"
fi

FAKE_BIN=$(mktemp -d)
trap 'rm -rf "$FAKE_BIN"' EXIT
cat > "$FAKE_BIN/curl" <<'EOF'
#!/bin/bash
url=${!#}
if [[ "$url" == */builds ]]; then
  cat >/dev/null
  printf '{"web_url":"https://buildkite.example/build/1"}\n'
else
  printf '{"default_branch":"main"}\n'
fi
EOF
cat > "$FAKE_BIN/sleep" <<'EOF'
#!/bin/bash
exit 0
EOF
chmod +x "$FAKE_BIN/curl" "$FAKE_BIN/sleep"

generator_output=$(PATH="$FAKE_BIN:$PATH" \
  BUILDKITE_ORGANIZATION_SLUG=test \
  BUILDKITE_API_TOKEN=test-token \
  "$ROOT/workload-generator.sh" run \
    --load=smoke \
    --cycles=1 \
    --builds-per-minute=60 \
    --prefix=demo \
    --commit=0123456789abcdef0123456789abcdef01234567 \
    --run-id=test-run)
assert_equal 6 "$(wc -l <<< "$generator_output" | tr -d ' ')"
assert_contains "$generator_output" \
  'pipeline=demo-dynamic-topology sequence=5 dynamic_queue=dynamic-blue'

trigger_output=$(PATH="$FAKE_BIN:$PATH" \
  BUILDKITE_ORGANIZATION_SLUG=test \
  BUILDKITE_API_TOKEN=test-token \
  "$ROOT/workload-generator.sh" trigger \
    --workload=late-queue \
    --prefix=demo \
    --commit=0123456789abcdef0123456789abcdef01234567 \
    --run-id=test-run)
assert_contains "$trigger_output" \
  'pipeline=demo-dynamic-topology sequence=1 dynamic_queue=dynamic-rare'

echo "workload harness tests passed"
