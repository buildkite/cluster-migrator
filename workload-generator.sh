#!/bin/bash

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

# shellcheck source=util/load-env.sh
source "$SCRIPT_DIR/util/load-env.sh"

die() {
  echo "workload-generator: $*" >&2
  exit 1
}

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

# NOTE: Kebab-cased pipeline name
DESIRED_PIPELINE_SLUG="cluster-migrator-demo"

# shellcheck source=util/workload-generator/functions.sh
source "$SCRIPT_DIR/util/workload-generator/functions.sh"

# Create a pipeline if it doesn't exist:
if ! demo_pipeline_exists; then
  create_demo_pipeline
fi

# Kick off builds at the configured rate
DEFAULT_BRANCH=$(jq -r '.default_branch' <<< "$PIPELINE")
SEQUENCE=1

while true; do
  create_demo_build
  SEQUENCE=$((SEQUENCE + 1))
  sleep "$BUILD_INTERVAL_SECONDS"
done
