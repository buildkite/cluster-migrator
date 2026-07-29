#!/bin/bash

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

# shellcheck source=util/load-env.sh
source "$SCRIPT_DIR/util/load-env.sh"

die() {
  echo "workload-generator: $*" >&2
  exit 1
}

usage() {
  cat <<'EOF'
Usage: ./workload-generator.sh run [--builds-per-minute=<1..60>]

Creates the cluster migration target, shared-queue consumer, and unrelated
control pipelines, then generates builds across all three at the total rate.
EOF
}

case "${1:-}" in
  run)
    shift
    ;;
  --help|-h)
    usage
    exit 0
    ;;
esac

BUILDS_PER_MINUTE=4
while (($#)); do
  case "$1" in
    --builds-per-minute=*)
      BUILDS_PER_MINUTE=${1#*=}
      ;;
    --builds-per-minute)
      [[ $# -ge 2 ]] || die "--builds-per-minute requires a value"
      BUILDS_PER_MINUTE=$2
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
  shift
done

if [[ ! "$BUILDS_PER_MINUTE" =~ ^[0-9]+$ ]] ||
  ((10#$BUILDS_PER_MINUTE < 1 || 10#$BUILDS_PER_MINUTE > 60)); then
  die "--builds-per-minute must be an integer from 1 to 60"
fi
BUILDS_PER_MINUTE=$((10#$BUILDS_PER_MINUTE))
BUILD_INTERVAL_SECONDS=$(awk -v rate="$BUILDS_PER_MINUTE" 'BEGIN { print 60 / rate }')

: "${BUILDKITE_API_TOKEN:?Set BUILDKITE_API_TOKEN in .env or the environment}"
BUILDKITE_API_URL=${BUILDKITE_API_URL:-http://api.buildkite.localhost/v2}

COMMON_CURL_ARGS=(
  --fail
  --silent
  --show-error
  --header "Authorization: Bearer $BUILDKITE_API_TOKEN"
)
if [[ -z ${BUILDKITE_ORGANIZATION_SLUG:-} ]]; then
  BUILDKITE_ORGANIZATION_SLUG=$(curl "${COMMON_CURL_ARGS[@]}" "$BUILDKITE_API_URL/organizations" | jq -r '.[0].slug')
fi
export BUILDKITE_ORGANIZATION_SLUG

# shellcheck source=util/workload-generator/functions.sh
source "$SCRIPT_DIR/util/workload-generator/functions.sh"

DEFAULT_BRANCHES=()
EVERYONE_TEAM_ID=""

for index in "${!WORKLOAD_PIPELINE_SLUGS[@]}"; do
  pipeline_slug=${WORKLOAD_PIPELINE_SLUGS[$index]}

  if ! PIPELINE=$(demo_pipeline "$pipeline_slug"); then
    if [[ -z "$EVERYONE_TEAM_ID" ]]; then
      EVERYONE_TEAM_ID=$(everyone_team_id)
      [[ -n "$EVERYONE_TEAM_ID" && "$EVERYONE_TEAM_ID" != "null" ]] ||
        die "the organization does not have an Everyone team"
    fi

    PIPELINE=$(create_demo_pipeline \
      "$pipeline_slug" \
      "${WORKLOAD_PIPELINE_NAMES[$index]}" \
      "${WORKLOAD_PIPELINE_DEFAULT_QUEUES[$index]}" \
      "${WORKLOAD_PIPELINE_FILES[$index]}" \
      "$EVERYONE_TEAM_ID")
  fi

  DEFAULT_BRANCHES+=("$(jq -r '.default_branch' <<< "$PIPELINE")")
done

SEQUENCE=1

while true; do
  create_workload_cycle
done
