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

`queue metrics` displays the latest and maximum destination activity over the server's observation window. This activity is operational context only: it does not establish pipeline readiness. Before increasing traffic, also inspect dispatch and queue latency and stranded-job alerts in observability. The observation time shows when the window ended. When the API provides `next_refresh_at`, the refresh countdown shows when the cached snapshot becomes eligible for request-driven replacement, not when a newer observation is guaranteed to be complete. Missing values are shown as `—`, not zero:

```text
QUEUE ACTIVITY
Source: default (unclustered)
Destination: default (cluster cluster-id)
Routing: 30%
Observed: 1 minute 52 seconds ago
Refresh due: in 8 seconds

METRIC            LATEST  10M MAX
Connected agents  50      54
Waiting jobs      4       12
Running jobs      38      46
```

With `--json`, the command returns the API response, including `null` values, routing percentage, and observation timestamps. During a mixed deployment or rollback, an API response may omit `next_refresh_at`; human-readable output then omits the refresh line.

When metrics are still being prepared, the command waits for the server's requested retry interval without writing to stdout. If preparation takes longer than the first retry, it reports progress on stderr and keeps retrying for up to one minute.

`pipeline readiness` separates queue blockers from concurrency-group blockers, shows observation coverage and freshness, and suggests a next command only when a blocker has a supported CLI action. Missing routing percentages and timestamps are shown as `—` or unavailable rather than zero. Machine reason identifiers remain visible beside their operator-readable descriptions:

```text
PIPELINE READINESS
Pipeline: monorepo
Destination cluster: cluster-id
Status: BLOCKED — 2 known blockers

QUEUE BLOCKERS (1)

QUEUE   ROUTING  ACTIVE SOURCE JOBS
deploy  80%      2

deploy
  - Routing is below 100% (routing_incomplete)
  - Active jobs remain on the source queue (active_source_jobs)

CONCURRENCY-GROUP BLOCKERS (1)

SCOPE     KEY
pipeline  deploy

pipeline / deploy
  - Migration is unavailable for this concurrency group (concurrency_group_migration_unavailable)

OBSERVATIONS
Queues: incomplete; 10-minute window ending 48 seconds ago; started 2026-09-01 06:50:00 UTC
Concurrency groups: incomplete; 10-minute window ending 48 seconds ago; started 2026-09-01 06:50:00 UTC

Incomplete observations may omit blockers outside the observed window.

NEXT

Review destination activity before increasing routing for deploy:

  cluster-migrator queue metrics deploy

Wait for active source jobs on deploy to finish, then reassess:

  cluster-migrator pipeline readiness monorepo --destination-cluster cluster-id
```

A blocked assessment is printed to stdout and still exits non-zero. `NO KNOWN BLOCKERS` means only that no blocker was found in the reported observations; it is not proof that the pipeline is ready to move or authorization to move it. Dependency discovery covers the reported recent window and may be incomplete. Current queue migration state is evaluated against a cached observation, while the assessment itself is never cached.

With `--json`, `pipeline readiness` returns the unchanged API-shaped object, including raw statuses and reasons, `null` values, observation timestamps, and the API URL. Terminal descriptions and next-step guidance are not added to JSON.

If an observation is being refreshed, the CLI waits without writing to stdout, reports prolonged preparation on stderr, and retries for up to one minute.

When `pipeline move --dry-run` is blocked, it prints the readiness assessment, including queue and concurrency-group blockers, and exits non-zero without printing a proposed move. With `--json`, stdout contains exactly one readiness assessment object.

A successful `pipeline move` completes when the synchronous move response returns and identifies the pipeline and destination cluster by name:

```text
RESULT

Demo Pipeline (demo-pipeline) is now using the Production cluster
```

The command verifies that the response reports the requested destination cluster. If the API omits the pipeline name, the slug is displayed instead. With `--json`, it prints the API-shaped response without a confirmation request. Dry runs retain the proposed-change output and do not report a completed move.

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

Human-readable queue mutations keep queue and destination identity outside the result, then show the routing transition on one line. Dry runs use the same layout with `RESULT (dry run)` and still make no mutation. For example:

```text
QUEUE CONFIGURE
Queue: default
Destination: Cluster Migrator Demo

RESULT

Routing: — → 0%

NEXT

Scale the destination infrastructure. When ready, begin routing:

  cluster-migrator queue set-percent default --to <percentage>
```

```text
QUEUE SET-PERCENT
Queue: default
Destination: Cluster Migrator Demo

RESULT

Routing: 10% → 25%

NEXT

Review destination activity before increasing routing:

  cluster-migrator queue metrics default
```

Rollback context is grouped under `SOURCE` rather than mixed into the routing result:

```text
QUEUE ROLLBACK
Queue: default
Destination: Cluster Migrator Demo

RESULT

Routing: 25% → 0%

SOURCE

New jobs will return to the unclustered queue. Existing jobs remain where they were routed.
```

A single-queue status keeps identity in its summary:

```text
QUEUE STATUS
Queue: default
Destination: Cluster Migrator Demo

RESULT

Routing: 25%
```

Status for all queues uses a table because each row has the same comparable fields:

```text
QUEUE STATUS
Migrations: 2

RESULT

QUEUE    ROUTING  DESTINATION
default  25%      Cluster Migrator Demo
deploy   100%     Production
```

The empty state uses the same hierarchy and puts its action under `NEXT`:

```text
QUEUE STATUS
Migrations: 0

RESULT

No queue migrations configured.

NEXT

Configure a queue migration:

  cluster-migrator queue configure <queue> --destination-cluster <cluster>
```

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
