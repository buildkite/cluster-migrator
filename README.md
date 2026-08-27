# Cluster Migrator

`cluster-migrator` safely moves Buildkite workloads from unclustered queues to a cluster. It configures queue routing, cuts over concurrency groups, checks pipeline readiness, and permanently moves ready pipelines.

> [!IMPORTANT]
> Queue migration APIs are implemented behind a Buildkite feature flag. The concurrency-group cutover and pipeline-readiness contracts are provisional until their server APIs ship. Do not use this CLI for a production migration until those contracts and their operational safety gates are complete.

## Build

```shell
go build ./cmd/cluster-migrator
```

Configure the organization and API token through the environment:

```shell
export BUILDKITE_ORGANIZATION_SLUG=<organization>
export BUILDKITE_API_TOKEN=<token>
```

## Migration sequence

Scale destination capacity before increasing queue traffic.

```shell
# 1. Map a source queue to an existing cluster queue at 0%.
cluster-migrator queue configure test \
  --destination-cluster production

# 2. Gradually route new jobs.
cluster-migrator queue set-percent test --to 10
cluster-migrator queue set-percent test --to 30
cluster-migrator queue set-percent test --to 100

# 3. Atomically hold, drain, and move a concurrency group.
cluster-migrator concurrency-group cutover deploy-production \
  --destination-cluster production \
  --wait

# 4. Inspect all queue and group blockers.
cluster-migrator pipeline readiness monorepo \
  --destination-cluster production

# 5. Permanently assign the pipeline to the cluster.
cluster-migrator pipeline move monorepo \
  --destination-cluster production \
  --wait
```

Use `cluster-migrator queue rollback test` to return newly created jobs to the unclustered queue. Existing jobs remain where they were originally routed.

Every mutation supports `--dry-run`. Use `--yes` for non-interactive operation and `--json` for machine-readable output. Percentages are absolute, not relative increments.

## Commands

```text
cluster-migrator queue configure
cluster-migrator queue set-percent
cluster-migrator queue rollback
cluster-migrator queue status

cluster-migrator concurrency-group cutover
cluster-migrator concurrency-group status

cluster-migrator pipeline readiness
cluster-migrator pipeline move
```

Queue migrations are addressed by their exact, case-sensitive source queue key. The CLI resolves destination cluster names to IDs and reads fresh server state before every mutation.

The CLI calls these queue migration endpoints:

```text
GET    /v2/organizations/{org}/cluster-queue-migrations
POST   /v2/organizations/{org}/cluster-queue-migrations
GET    /v2/organizations/{org}/cluster-queue-migrations/{queue_key}
PATCH  /v2/organizations/{org}/cluster-queue-migrations/{queue_key}
DELETE /v2/organizations/{org}/cluster-queue-migrations/{queue_key}
```

The intended concurrency-group and pipeline endpoints remain in the `cluster-queue-migrations` namespace. Their provisional contracts are isolated in `internal/buildkite` so they can change without affecting command parsing.
