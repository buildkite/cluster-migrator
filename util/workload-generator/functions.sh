#!/bin/bash

WORKLOAD_PIPELINE_SLUGS=(
  "cluster-migrator-target"
  "cluster-migrator-shared-consumer"
  "cluster-migrator-control"
)
WORKLOAD_PIPELINE_NAMES=(
  "Cluster Migrator Target"
  "Cluster Migrator Shared Queue Consumer"
  "Cluster Migrator Unrelated Control"
)
WORKLOAD_PIPELINE_DEFAULT_QUEUES=(
  "target-only"
  "shared"
  "control-only"
)
WORKLOAD_PIPELINE_FILES=(
  "util/workload-generator/.buildkite/target/pipeline.yml"
  "util/workload-generator/.buildkite/shared-consumer/pipeline.yml"
  "util/workload-generator/.buildkite/control/pipeline.yml"
)

demo_pipeline() {
  local pipeline_slug=$1

  curl "${COMMON_CURL_ARGS[@]}" \
    "$BUILDKITE_API_URL/organizations/$BUILDKITE_ORGANIZATION_SLUG/pipelines/$pipeline_slug" 2>/dev/null
}

everyone_team_id() {
  curl "${COMMON_CURL_ARGS[@]}" \
    "$BUILDKITE_API_URL/organizations/$BUILDKITE_ORGANIZATION_SLUG/teams" |
    jq -r '.[] | select(.name == "Everyone") | .id'
}

create_demo_pipeline() {
  local pipeline_slug=$1
  local pipeline_name=$2
  local default_queue=$3
  local pipeline_file=$4
  local team_id=$5

  jq -n \
    --arg slug "$pipeline_slug" \
    --arg name "$pipeline_name" \
    --arg default_queue "$default_queue" \
    --arg pipeline_file "$pipeline_file" \
    --arg team_id "$team_id" \
    '{
      name: $name,
      slug: $slug,
      repository: "https://github.com/buildkite/cluster-migrator",
      default_branch: "initial-cluster-migrator-tooling",
      cluster_id: null,
      teams: {($team_id): "manage_build_and_read"},
      configuration: ("agents:\n  queue: \"" + $default_queue + "\"\n\nsteps:\n  - label: \"Pipeline upload\"\n    command: \"buildkite-agent pipeline upload " + $pipeline_file + "\"")
    }' |
    curl "${COMMON_CURL_ARGS[@]}" \
      --request POST \
      --header "Content-Type: application/json" \
      --data-binary @- \
      "$BUILDKITE_API_URL/organizations/$BUILDKITE_ORGANIZATION_SLUG/pipelines"
}

create_demo_build() {
  local pipeline_slug=$1
  local default_branch=$2
  local sequence=$3

  jq -n \
    --arg branch "$default_branch" \
    --arg message "Cluster migration workload $sequence" \
    '{commit: "HEAD", branch: $branch, message: $message}' |
    curl "${COMMON_CURL_ARGS[@]}" \
      --request POST \
      --header "Content-Type: application/json" \
      --data-binary @- \
      "$BUILDKITE_API_URL/organizations/$BUILDKITE_ORGANIZATION_SLUG/pipelines/$pipeline_slug/builds" |
    jq -r '.web_url'
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
