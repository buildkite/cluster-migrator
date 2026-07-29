#!/bin/bash

WORKLOAD_PIPELINE_SLUGS=(
  "cluster-migrator-target"
  "cluster-migrator-shared-consumer"
  "cluster-migrator-control"
)

demo_pipeline() {
  local pipeline_slug=$1

  curl "${COMMON_CURL_ARGS[@]}" \
    "$BUILDKITE_API_URL/organizations/$BUILDKITE_ORGANIZATION_SLUG/pipelines/$pipeline_slug" 2>/dev/null
}

create_demo_build() {
  local pipeline_slug=$1
  local default_branch=$2
  local sequence=$3
  local response web_url

  response=$(jq -n \
    --arg branch "$default_branch" \
    --arg message "Cluster migration workload $sequence" \
    '{commit: "HEAD", branch: $branch, message: $message}' |
    curl "${COMMON_CURL_ARGS[@]}" \
      --request POST \
      --header "Content-Type: application/json" \
      --data-binary @- \
      "$BUILDKITE_API_URL/organizations/$BUILDKITE_ORGANIZATION_SLUG/pipelines/$pipeline_slug/builds") ||
    return 1

  if ! web_url=$(jq -er \
      '.web_url | select(type == "string" and length > 0)' <<< "$response"); then
    printf 'workload-generator: create build response for pipeline %s did not include web_url\n' \
      "$pipeline_slug" >&2
    return 1
  fi

  printf '%s\n' "$web_url"
}

create_workload_cycle() {
  local index

  for index in "${!WORKLOAD_PIPELINE_SLUGS[@]}"; do
    create_demo_build \
      "${WORKLOAD_PIPELINE_SLUGS[$index]}" \
      "${DEFAULT_BRANCHES[$index]}" \
      "$SEQUENCE"
    SEQUENCE=$((SEQUENCE + 1))
    sleep "$BUILD_INTERVAL_SECONDS"
  done
}
