# Cluster Migrator

`cluster-migrator` moves Buildkite workloads from unclustered queues to a cluster without an all-at-once cutover. It gradually routes new jobs to cluster queues, compares source and destination activity, cuts over concurrency groups, checks known pipeline blockers, and permanently assigns pipelines to the destination cluster.

> [!IMPORTANT]
> The queue migration APIs require a Buildkite feature flag. The concurrency-group cutover and pipeline migration contracts remain provisional. Do not use this tool for a production migration until Buildkite has enabled the APIs and confirmed the operational safety gates for your organization.

## How it works

A migration has three stages:

1. **Route queues.** Map each unclustered source queue to an existing queue with the same key in the destination cluster, then increase the percentage of new jobs sent there from 0% to 100%.
2. **Cut over concurrency groups.** Hold, drain, and move each concurrency group once its queue blockers are cleared.
3. **Move pipelines.** Once every queue used by a pipeline is fully routed and active source jobs have drained, assess its queue and concurrency-group blockers before permanently assigning it to the cluster.

Routing changes affect new jobs only. Jobs already created remain on the queue selected when they were created.

You can route queues used by pipelines with concurrency groups, but do not move those pipelines until their concurrency groups have also moved to the cluster. Pipeline readiness reports unmigrated concurrency groups as blockers.

## Prerequisites

Before starting:

- Ask Buildkite to enable the queue migration APIs for the organization.
- Enable Advanced Queue Metrics for the organization so `queue metrics` can monitor source and destination activity.
- Create the destination cluster and its queues. Each destination queue must have the same exact, case-sensitive key as its source queue.
- Prepare enough agent capacity in the destination cluster for the traffic you will route.
- Create an API token that can access exactly one Buildkite organization with these scopes:

  - `read_organizations`
  - `read_clusters`
  - `write_clusters`
  - `read_pipelines`
  - `write_pipelines`

  Export the token:

  ```shell
  export BUILDKITE_API_TOKEN=<token>
  ```

The CLI discovers the organization from the token and caches the slug locally. It refreshes a stale cache entry automatically.

## Install

Install the latest release with [mise](https://mise.jdx.dev/):

```shell
mise use --global github:buildkite/cluster-migrator
```

Alternatively, download a binary from [GitHub Releases](https://github.com/buildkite/cluster-migrator/releases). Releases support macOS and Linux on x86-64 and ARM64.

## Migrate a workload

The examples below migrate the `test` queue and the `monorepo` pipeline to the `production` cluster. Use a cluster name or ID and a pipeline slug, exact name, or ID.

### 1. Inspect current migrations

```shell
cluster-migrator queue status
```

Pass a queue key to inspect one migration:

```shell
cluster-migrator queue status test
```

### 2. Configure the queue at 0%

Preview the change, then create the mapping:

```shell
cluster-migrator --dry-run queue configure test \
  --destination-cluster production

cluster-migrator queue configure test \
  --destination-cluster production
```

Configuration does not route traffic. Scale the destination agents before raising the percentage.

### 3. Increase routing gradually

Percentages are absolute, not relative increments:

```shell
cluster-migrator queue set-percent test --to 10
cluster-migrator queue metrics test

cluster-migrator queue set-percent test --to 30
cluster-migrator queue metrics test

cluster-migrator queue set-percent test --to 100
```

Before each increase, confirm the destination has enough connected agents, waiting jobs remain controlled, and p95 wait time is acceptable. `queue metrics` compares the latest source activity with the latest and peak destination activity over the reported observation window. It provides context, not proof that a queue is safe to increase; also check your normal dispatch, queue-latency, and stranded-job observability.

Routing changes take effect when jobs are created, so observed traffic may take time to reflect the new percentage. For an infrequently used queue, trigger a representative build before continuing. Once the destination is healthy, safely reduce the corresponding source-agent capacity before provisioning and routing the next increment.

To stop sending new jobs to the cluster queue:

```shell
cluster-migrator queue rollback test
```

Rollback sets routing to 0%. It does not move existing jobs or remove the queue mapping.

### 4. Cut over concurrency groups

For workloads using concurrency groups, hold, drain, and move each group after its queue blockers are cleared:

```shell
cluster-migrator concurrency-group cutover deploy-production \
  --destination-cluster production \
  --wait
```

Use `concurrency-group status [group]` to inspect one group or list all groups. These commands require the provisional server-side concurrency-group APIs.

### 5. Assess each pipeline

After all queues used by a pipeline reach 100%, check its known blockers:

```shell
cluster-migrator pipeline readiness monorepo \
  --destination-cluster production
```

The assessment reports queue blockers, active source jobs, concurrency-group blockers, observation windows, and when the observations can refresh. A `no_known_blockers` result is not proof of readiness: incomplete observation windows may omit dependencies.

A blocked assessment prints corrective next steps and exits non-zero.

After a pipeline moves, step uploads that target a queue missing from the destination cluster will fail. Check static and dynamically generated pipeline steps for queue keys that may not appear in the readiness observation window.

### 6. Move the pipeline

Previewing a move runs the readiness assessment without changing the pipeline:

```shell
cluster-migrator --dry-run pipeline move monorepo \
  --destination-cluster production
```

Move the pipeline only after reviewing the assessment and your operational signals:

```shell
cluster-migrator pipeline move monorepo \
  --destination-cluster production
```

The CLI has no pipeline rollback command. The server revalidates authoritative migration invariants when applying a move; an earlier `no_known_blockers` result is not authorization by itself.

Repeat the queue stages for every source queue and the pipeline stages for every pipeline in the workload.

## Command reference

| Command | Purpose |
| --- | --- |
| `queue configure <queue> --destination-cluster <cluster>` | Map a source queue to an existing destination queue at 0%. |
| `queue status [queue]` | Show one queue migration or list all migrations. |
| `queue set-percent <queue> --to <0-100>` | Set the absolute percentage of new jobs routed to the cluster queue. |
| `queue metrics <queue>` | Compare recent source and destination activity. |
| `queue rollback <queue>` | Set routing to 0% for new jobs. |
| `concurrency-group cutover <group> --destination-cluster <cluster> [--wait]` | Hold, drain, and move a concurrency group; optionally wait for completion. |
| `concurrency-group status [group]` | Show one concurrency group or list all groups. |
| `pipeline readiness <pipeline> --destination-cluster <cluster>` | Assess known queue and concurrency-group blockers. |
| `pipeline move <pipeline> --destination-cluster <cluster>` | Permanently assign a pipeline to the cluster. |

Run `cluster-migrator <command> --help` for full usage.

### Global flags

| Flag | Purpose |
| --- | --- |
| `--dry-run` | Validate and display a mutation without applying it. |
| `--json` | Write machine-readable JSON instead of terminal output. |
| `--timeout <duration>` | Set the maximum command duration; defaults to `10m`. |
| `--endpoint <url>` | Override the Buildkite REST API endpoint. Also available as `BUILDKITE_API_ENDPOINT`. |
| `--api-token <token>` | Supply the API token. Prefer `BUILDKITE_API_TOKEN` to keep it out of shell history and process listings. |

All mutations are non-interactive. The CLI reads fresh server state before applying routing changes.

## Output and automation

Human-readable output includes current state, results, observation freshness, and suggested next steps. Missing metric values appear as `—`, not zero.

Use `--json` in scripts:

```shell
cluster-migrator --json queue status
cluster-migrator --json queue metrics test
cluster-migrator --json pipeline readiness monorepo \
  --destination-cluster production
```

JSON preserves the API's `null` metric and observation values. Terminal-only guidance is omitted.

Queue metrics and pipeline readiness may initially be unavailable while Buildkite prepares an observation. The CLI follows the server's retry interval for up to one minute, writes prolonged-wait progress to stderr, and keeps stdout clean for the final human-readable or JSON result.

## Development

Install the pinned tools, build the binary, and run the development checks with [mise](https://mise.jdx.dev/):

```shell
mise install
mise run build
mise run format
mise run lint
mise run test
mise run vet
mise run vulnerability
```

The build task creates `./cluster-migrator`. Run `mise run release-check` when changing the release configuration. The pre-commit hook runs formatting, linting, tests, and `go vet` in parallel.

## License

MIT. See [LICENSE](LICENSE).
