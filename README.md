# Cluster Migrator

`cluster-migrator` safely moves Buildkite workloads from unclustered queues to a cluster. It configures queue routing, assesses known pipeline blockers, and permanently moves pipelines.

> [!IMPORTANT]
> Queue migration APIs are implemented behind a Buildkite feature flag. The pipeline-readiness contract is provisional until its server API ships. Do not use this CLI for a production migration until that contract and its operational safety gates are complete.

## Install

Install the latest release with [mise](https://mise.jdx.dev/):

```shell
mise use --global github:buildkite/cluster-migrator
```

Prebuilt releases support macOS and Linux on x86-64 and ARM64.

## Build and test

Install the development tools, build the executable, and run the tests with mise:

```shell
mise install
mise run build
mise run test
```

The build task creates `./cluster-migrator`.

Configure an organization-scoped API token through the environment:

```shell
export BUILDKITE_API_TOKEN=<token>
```

The CLI discovers the organization slug from the token and caches it locally. If the cached organization is no longer available, the CLI refreshes it automatically.

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

# 3. Check recent destination activity for operational context.
cluster-migrator queue metrics test

# Before increasing traffic, also inspect dispatch and queue latency plus
# stranded-job alerts in your observability tools.

# 4. Assess known blockers using the pipeline ID, name, or slug.
cluster-migrator pipeline readiness monorepo \
  --destination-cluster production

# 5. Permanently assign the pipeline to the cluster.
cluster-migrator pipeline move monorepo \
  --destination-cluster production
```

Use `cluster-migrator queue rollback test` to return newly created jobs to the unclustered queue. Existing jobs remain where they were originally routed.

Every mutation runs non-interactively and supports `--dry-run`. Use `--json` for machine-readable output. Percentages are absolute, not relative increments.

## Commands

```text
cluster-migrator queue configure
cluster-migrator queue set-percent
cluster-migrator queue rollback
cluster-migrator queue status
cluster-migrator queue metrics

cluster-migrator pipeline readiness
cluster-migrator pipeline move
```

Queue migrations are addressed by their exact, case-sensitive source queue key. The CLI resolves destination cluster names to IDs and reads fresh server state before every mutation.

The CLI calls these queue migration endpoints:

```text
GET    /v2/organizations/{org}/cluster-queue-migrations
POST   /v2/organizations/{org}/cluster-queue-migrations
GET    /v2/organizations/{org}/cluster-queue-migrations/{queue_key}
GET    /v2/organizations/{org}/cluster-queue-migrations/{queue_key}/metrics
PATCH  /v2/organizations/{org}/cluster-queue-migrations/{queue_key}
DELETE /v2/organizations/{org}/cluster-queue-migrations/{queue_key}

GET    /v2/organizations/{org}/cluster-queue-migrations/pipelines/{pipeline}/readiness
POST   /v2/organizations/{org}/cluster-queue-migrations/pipelines/{pipeline}/move
```

Percentage changes through the queue migration `PATCH` endpoint must atomically enforce capacity and dependency gates. The future `move` endpoint must atomically validate authoritative invariants on the writer before changing a pipeline's cluster; a prior `no_known_blockers` assessment is not authorization to move. These guarded endpoints and the pipeline contracts are provisional until their server implementations ship. They are isolated in `internal/buildkite` so they can change without affecting command parsing.
