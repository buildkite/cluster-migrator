#!/bin/bash

set -euo pipefail
umask 077

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ENV_FILE="$SCRIPT_DIR/.env"

# shellcheck source=util/common.sh
source "$SCRIPT_DIR/util/common.sh"

die() {
  echo "setup-organization: $*" >&2
  exit 1
}

usage() {
  cat <<'EOF'
Usage: ./setup-organization.sh [--branch=<branch>]

Creates or reuses the demo cluster, queues, and pipelines, then creates any
missing clustered and unclustered agent credentials. Generated values are
written to the repository's gitignored .env file with mode 0600.

New and existing demo pipelines must use the requested default branch. The
branch defaults to main.
EOF
}

PIPELINE_BRANCH=main
while (($#)); do
  case "$1" in
    --branch=*)
      PIPELINE_BRANCH=${1#*=}
      ;;
    --branch)
      [[ $# -ge 2 ]] || die "--branch requires a value"
      PIPELINE_BRANCH=$2
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
[[ -n "$PIPELINE_BRANCH" ]] || die "--branch requires a non-empty value"

: "${BUILDKITE_ORGANIZATION_SLUG:?Set BUILDKITE_ORGANIZATION_SLUG in .env or the environment}"
: "${BUILDKITE_API_TOKEN:?Set BUILDKITE_API_TOKEN in .env or the environment}"
BUILDKITE_API_URL=${BUILDKITE_API_URL:-https://api.buildkite.com/v2}
BUILDKITE_GRAPHQL_URL=${BUILDKITE_GRAPHQL_URL:-https://graphql.buildkite.com/v1}
API_AUTH_HEADER_FILE=$(create_curl_auth_header_file Bearer "$BUILDKITE_API_TOKEN") ||
  die "could not create a protected API authentication file"
trap 'rm -f "$API_AUTH_HEADER_FILE"' EXIT

COMMON_CURL_ARGS=(
  --fail
  --silent
  --show-error
  --header "@$API_AUTH_HEADER_FILE"
)
export BUILDKITE_ORGANIZATION_SLUG

# shellcheck source=util/workload-generator/functions.sh
source "$SCRIPT_DIR/util/workload-generator/functions.sh"
# shellcheck source=util/setup-organization/functions.sh
source "$SCRIPT_DIR/util/setup-organization/functions.sh"

CLUSTER=$(ensure_cluster "$SETUP_CLUSTER_NAME")
DESTINATION_CLUSTER_UUID=$(jq -er '.id' <<< "$CLUSTER")
save_env_value "$ENV_FILE" DESTINATION_CLUSTER_UUID "$DESTINATION_CLUSTER_UUID"

while IFS= read -r queue_key; do
  ensure_cluster_queue "$DESTINATION_CLUSTER_UUID" "$queue_key" >/dev/null
done < <(printf '%s\n' "${WORKLOAD_PIPELINE_DEFAULT_QUEUES[@]}" | sort -u)

EVERYONE_TEAM_ID=$(everyone_team_id)
[[ -n "$EVERYONE_TEAM_ID" && "$EVERYONE_TEAM_ID" != "null" ]] ||
  die "the organization does not have an Everyone team"

for index in "${!WORKLOAD_PIPELINE_SLUGS[@]}"; do
  ensure_demo_pipeline \
    "${WORKLOAD_PIPELINE_SLUGS[$index]}" \
    "${WORKLOAD_PIPELINE_NAMES[$index]}" \
    "${WORKLOAD_PIPELINE_DEFAULT_QUEUES[$index]}" \
    "${WORKLOAD_PIPELINE_FILES[$index]}" \
    "$EVERYONE_TEAM_ID" \
    "$PIPELINE_BRANCH" >/dev/null
done

if [[ -z ${BUILDKITE_CLUSTER_AGENT_TOKEN:-} ]]; then
  CLUSTER_TOKEN_RESPONSE=$(create_cluster_agent_token \
    "$DESTINATION_CLUSTER_UUID" \
    "$SETUP_CLUSTER_TOKEN_DESCRIPTION")
  BUILDKITE_CLUSTER_AGENT_TOKEN=$(jq -er '.token' <<< "$CLUSTER_TOKEN_RESPONSE")
  save_env_value "$ENV_FILE" BUILDKITE_CLUSTER_AGENT_TOKEN "$BUILDKITE_CLUSTER_AGENT_TOKEN"
fi

if [[ -z ${BUILDKITE_UNCLUSTERED_AGENT_TOKEN:-} ]]; then
  ORGANIZATION_GRAPHQL_ID=$(organization_graphql_id "$BUILDKITE_ORGANIZATION_SLUG")
  UNCLUSTERED_TOKEN_RESPONSE=$(create_unclustered_agent_token \
    "$ORGANIZATION_GRAPHQL_ID" \
    "$SETUP_UNCLUSTERED_TOKEN_DESCRIPTION")
  BUILDKITE_UNCLUSTERED_AGENT_TOKEN=$(jq -er '.data.agentTokenCreate.tokenValue' <<< "$UNCLUSTERED_TOKEN_RESPONSE")
  save_env_value "$ENV_FILE" BUILDKITE_UNCLUSTERED_AGENT_TOKEN "$BUILDKITE_UNCLUSTERED_AGENT_TOKEN"
fi

printf 'Setup complete for cluster %s. Credentials were saved to .env.\n' \
  "$DESTINATION_CLUSTER_UUID"
