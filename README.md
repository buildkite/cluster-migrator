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

## Output

Queue mutations display the previous and resulting routing percentages. Successful configuration, percentage changes below 100%, and single-queue status at 0% also identify the next migration step. Status for all queues remains informational.

When no queue migrations are configured, human-readable `queue status` points to `queue configure`; with `--json`, it returns `[]`.

`queue metrics` displays current source activity separately from the latest and maximum destination activity. These counts are workload context, not required-agent estimates or a pipeline readiness assessment. Before increasing traffic, also inspect dispatch and queue latency and stranded-job alerts in observability. When the API provides `next_refresh_at`, the refresh countdown shows when the cached destination snapshot becomes eligible for request-driven replacement, not when a newer observation is guaranteed to be complete. Missing values are shown as `—`, not zero:

```text
QUEUE METRICS
Queue: default
Routing: 30%

SOURCE ACTIVITY (Unclustered)
Observed: just now

METRIC        CURRENT
Waiting jobs  16
Running jobs  31

DESTINATION ACTIVITY (Cluster cluster-id)
Observed: 1 minute 52 seconds ago
Refresh due: in 8 seconds

METRIC            LATEST  10M MAX
Connected agents  50      54
Waiting jobs      4       12
Running jobs      38      46
```

Destination activity comes from the existing metrics window; its observation time shows when that window ended. Source activity is queried separately for each request from the Pipelines replica, so its observation time and freshness can differ and replica lag can delay its counts. Source waiting jobs are active unclustered script jobs in `scheduled`, `reserved`, `assigned`, and `accepted`; source running jobs are those in `running`, `canceling`, and `timing_out`. The counts include active jobs created before migration and jobs left unclustered by routing when they were created, but exclude destroyed and already-routed jobs. Changing the routing percentage does not move existing jobs.

Historical source peaks and throughput are unavailable, so the source table shows only current workload and the API returns `null` source peaks. With `--json`, the command preserves the API response shape, including the required `source` object, those `null` values, routing percentage, refresh time, and both observation timestamps. A missing or `null` `source` is an API contract error and produces no command output.

Complete the backend rollout that provides `source` before releasing this client. Rolling the backend back to a version without `source` after this client is released is unsupported and produces the contract error rather than degraded destination-only output. The optional `next_refresh_at` may still be absent; human-readable output then omits the refresh line.

When metrics are still being prepared, the command waits for the server's requested retry interval without writing to stdout. If preparation takes longer than the first retry, it reports progress on stderr and keeps retrying for up to one minute.

`pipeline readiness` reports either `blocked` or `no_known_blockers`. The latter is not proof that the pipeline is ready to move: dependency discovery covers the reported recent window and is explicitly incomplete. Current queue migration state is evaluated against a cached observation, while the assessment itself is never cached. If an observation is being refreshed, the CLI waits without writing to stdout, reports prolonged preparation on stderr, and retries for up to one minute.

When `pipeline move --dry-run` is blocked, it prints the readiness assessment, including queue and concurrency-group blockers, and exits non-zero without printing a proposed move. With `--json`, stdout contains exactly one readiness assessment object.

A successful `pipeline move` completes when the synchronous move response returns. The command verifies that the response reports the requested destination cluster and, with `--json`, prints that API response without a confirmation request.

With `--json`, percentage changes and rollbacks return:

```json
{
  "queue": "default",
  "from_percent": 10,
  "to_percent": 25,
  "destination": {
    "cluster_id": "cluster-id",
    "cluster_name": "Cluster Migrator Demo"
  },
  "dry_run": false
}
```

Initial configuration uses the same shape without `from_percent`. Queue status returns:

```json
{
  "queue": "default",
  "routing_percent": 25,
  "destination": {
    "cluster_id": "cluster-id",
    "cluster_name": "Cluster Migrator Demo"
  }
}
```

Status for all queues returns an array of these objects. Terminal-only guidance is never included in JSON.

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
