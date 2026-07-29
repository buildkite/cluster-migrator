#!/bin/bash

demo_pipeline_exists() {
  PIPELINE=$(curl "${COMMON_CURL_ARGS[@]}" \
    "$BUILDKITE_API_URL/organizations/$BUILDKITE_ORGANIZATION_SLUG/pipelines/$DESIRED_PIPELINE_SLUG" 2>/dev/null)
}

create_demo_pipeline() {
  local everyone_team_id

  everyone_team_id=$(curl "${COMMON_CURL_ARGS[@]}" \
    "$BUILDKITE_API_URL/organizations/$BUILDKITE_ORGANIZATION_SLUG/teams" |
    jq -r '.[] | select(.name == "Everyone") | .id')

  PIPELINE=$(jq -n \
    --arg slug "$DESIRED_PIPELINE_SLUG" \
    --arg team_id "$everyone_team_id" \
    '{
      name: "Cluster Migrator Demo",
      slug: $slug,
      repository: "/Users/mitchsmith/github.com/buildkite/cluster-migrator.git",
      cluster_id: null,
      teams: {($team_id): "manage_build_and_read"},
      configuration: "agents:\n  queue: \"step-uploads\"\n\nsteps:\n  - label: \"Pipeline upload\"\n    command: \"buildkite-agent pipeline upload util/workload-generator/.buildkite/pipeline.yml\""
    }' |
    curl "${COMMON_CURL_ARGS[@]}" \
      --request POST \
      --header "Content-Type: application/json" \
      --data-binary @- \
      "$BUILDKITE_API_URL/organizations/$BUILDKITE_ORGANIZATION_SLUG/pipelines")
}

create_demo_build() {
  jq -n \
    --arg branch "$DEFAULT_BRANCH" \
    --arg message "Cluster migration workload $SEQUENCE" \
    '{commit: "HEAD", branch: $branch, message: $message}' |
    curl "${COMMON_CURL_ARGS[@]}" \
      --request POST \
      --header "Content-Type: application/json" \
      --data-binary @- \
      "$BUILDKITE_API_URL/organizations/$BUILDKITE_ORGANIZATION_SLUG/pipelines/$DESIRED_PIPELINE_SLUG/builds" |
    jq -r '.web_url'
}
