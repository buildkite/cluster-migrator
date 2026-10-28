# Cluster Migrator

`cluster-migrator` moves Buildkite workloads from unclustered queues to a cluster without an all-at-once cutover. It gradually routes new jobs to cluster queues, compares source and destination activity, cuts over concurrency groups, checks known pipeline blockers, and permanently assigns pipelines to the destination cluster.

> [!IMPORTANT]
> The queue migration APIs require a Buildkite feature flag. The concurrency-group cutover and pipeline migration contracts remain provisional. Do not use this tool for a production migration until Buildkite has enabled the APIs and confirmed the operational safety gates for your organization.

## How it works

A migration has three stages. Finish each stage for the whole organization before starting the next:

1. **Prepare and validate queues.** Map every unclustered source queue to an existing queue with the same key in the organization's fixed destination cluster, then increase the percentage of new jobs sent there from 0% to 100%. Routing remains reversible during preparation, including decreases from 100%. Explicit validation rejects unmapped source queues and freezes configuration until an explicit return to preparation.
2. **Cut over concurrency groups.** Only after the organization commits to concurrency-group cutover can groups move their authority to the cluster. Fully routing queues does not authorize commitment. This CLI does not yet expose the organization-wide commitment operation; coordinate that later stage with Buildkite.
3. **Move pipelines.** Assess each pipeline's queue and concurrency-group blockers, then permanently assign it to the cluster. Repeat for every pipeline.

Routing changes affect new jobs only. Jobs already created remain on the queue selected when they were created.

New concurrency groups retain unclustered authority throughout preparation and validation, even at 100% routing. Do not move a pipeline until the organization reaches the pipeline-migration phase and every concurrency group it uses is in the cluster. Pipeline readiness reports known blockers.

Concurrency groups are identified as `<scope>/<key>`: `custom/<key>` for a `concurrency_group` set in pipeline YAML, or `step/<id>` for a group Buildkite created for a step. Equal keys in different scopes are different groups. Keys may contain slashes, such as `custom/deploy/production`.

## Prerequisites

Before starting:

- Ask Buildkite to configure the organization's migration and fixed destination and enable the queue migration APIs. Enrollment is not performed implicitly by queue commands.
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

The examples below use the `test` queue and an organization migration configured for the `production` cluster. Later pipeline examples use the `monorepo` pipeline. Use a pipeline slug, exact name, or ID.

### 1. Inspect current migrations

```shell
cluster-migrator migration status
cluster-migrator queue status
```

Pass a queue key to inspect one migration:

```shell
cluster-migrator queue status test
```

### 2. Configure the queue at 0%

Preview the change, then create the mapping:

```shell
cluster-migrator --dry-run queue configure test

cluster-migrator queue configure test
```

Configuration does not route traffic. Scale the destination agents before raising the percentage.

Queue configuration inherits the organization's fixed destination; `--destination-cluster` is not accepted. Configuration and percentage edits, including previews and same-percentage requests, require preparation. During validation, first run `cluster-migrator migration set-phase prepare-queues`; configure or adjust routing, then run `cluster-migrator migration set-phase validate-queues` again. After commitment, recover forward instead of trying to reopen preparation.

A missing organization migration means enrollment and its destination must be configured by Buildkite. A missing cluster queue migration means that queue has no mapping yet; `queue configure <queue>` creates it during preparation. Creating a queue mapping does not enroll the organization or choose a destination.

### 3. Increase routing gradually

Percentages are absolute, not relative increments:

```shell
cluster-migrator queue set-percent test --to 10
cluster-migrator queue metrics test

cluster-migrator queue set-percent test --to 30
cluster-migrator queue metrics test

cluster-migrator queue set-percent test --to 100
```

Setting 100% remains reversible during preparation and does not switch concurrency authority. Validate representative workloads before coordinating irreversible commitment with Buildkite.

Before each increase, confirm the destination has enough connected agents, waiting jobs remain controlled, and p95 wait time is acceptable. `queue metrics` compares the latest source activity with the latest and peak destination activity over the reported observation window. It provides context, not proof that a queue is safe to increase; also check your normal dispatch, queue-latency, and stranded-job observability.

Routing changes take effect when jobs are created, so observed traffic may take time to reflect the new percentage. For an infrequently used queue, trigger a representative build before continuing. Once the destination is healthy, safely reduce the corresponding source-agent capacity before provisioning and routing the next increment.

During preparation, stop sending new jobs to the cluster queue, including after reaching 100%:

```shell
cluster-migrator queue set-percent test --to 0
```

This does not move existing jobs or remove the queue mapping. Routing is frozen outside preparation.

### 4. Cut over concurrency groups

These retained individual commands are provisional diagnostics and operations for later phases, not the organization-wide commitment workflow. Do not use them to bypass validation or commitment. Coordinate commitment with Buildkite first; the server rejects individual cutovers during preparation and validation. Once committed and every queue is routed 100%, cut over every concurrency group that has been used on unclustered queues before moving any pipeline. Ask Buildkite for the list of these groups. Pipeline readiness also reports any group a pipeline still needs.

Request a cutover for one group:

```shell
cluster-migrator concurrency-group cutover custom/deploy
```

Like queue configuration, cutover uses the organization's fixed destination. Neither command accepts `--destination-cluster` or sends a destination in its mutation request. The server must resolve it from the organization migration.

To request cutovers for a list of groups in `groups.txt`, one per line:

```shell
while IFS= read -r group; do
  cluster-migrator concurrency-group cutover "$group" </dev/null
done < groups.txt
```

Each cutover runs on the server, so the loop doesn't wait for them to finish. Rerun the command for any group that was rejected once its blockers are resolved.

Buildkite checks the cutover before starting it, and rejects it without changes when:

- The group already has a migration that is in progress, finished, or targets another cluster.
- Any queue the group used in the last 10 minutes, or still has unfinished jobs on, is not routed 100% to the destination.
- Ungrouped jobs were created on those unclustered queues in the last 10 minutes. Older ungrouped jobs still running there don't block the cutover, but pipeline readiness blocks their pipelines until they finish. Jobs in other concurrency groups don't block, because they only leave the queue when their own group cuts over.

A rejected cutover prints its blockers and next steps and exits non-zero. Once they're resolved, run the same command again.

An accepted cutover holds the group, waits for its running unclustered jobs to finish, and then moves its waiting jobs to the cluster. The command returns once the cutover starts; the rest runs on the server. Check progress at any time:

```shell
cluster-migrator concurrency-group status custom/deploy
```

Concurrency-group migrations only move forward. Once a group routes to the cluster, it cannot return to unclustered queues. A forward migration cancelled before routing switches remains unclustered and can cut over again to the same destination.

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

Prepare and validate queue mappings first. Coordinate irreversible commitment with Buildkite before cutting over concurrency groups, then assess and move pipelines only in the pipeline-migration phase.

## Command reference

### Configure an organization migration

When Buildkite makes organization enrollment available, configure the fixed
destination before preparing and validating queue mappings:

```shell
cluster-migrator migration configure --destination-cluster production
cluster-migrator migration status
```

The destination accepts a cluster name or ID, using the organization discovered
from the API token. Configuration creates a migration in `prepare_queues`; it
does not create clusters, queues, agents, or queue mappings, or change routing
percentages. The destination cannot be changed by this command. An existing
migration returns a conflict even for the same destination; inspect it with
`migration status` rather than treating configuration as an idempotent update.

Configuration requires `write_clusters`, permission to change the organization
and manage the destination cluster, and server-side enrollment availability.
Cluster resolution also requires `read_clusters`. Enrollment is currently closed
on the server; this command does not enable it or bypass its rejection.

There is no read-only configuration readiness endpoint, so `migration configure
--dry-run` exits non-zero without a configuration request. `--json` returns the
API migration object on success; rejected requests leave stdout empty and report
the server error on stderr.

### Queue validation for configured organizations

All phase transitions are grouped under `migration set-phase <phase>`. Phase
names use hyphens in commands and underscores in API responses. The server still
enforces the allowed transition order and readiness checks.

The queue-validation APIs add an explicit, reversible preparation loop. They must
be deployed and enabled before using these commands. Configuration does not
enable server-side enrollment protections or authorize later transitions.

```shell
cluster-migrator migration status
cluster-migrator migration set-phase validate-queues
```

Validation rejects unmapped source queues in new builds, dynamic uploads,
retries, and parallelism increases. Mapped workloads continue at their configured
routing percentages, including percentages below 100%. Queue mappings and routing
percentages are frozen during validation. Neither validation nor 100% routing
switches concurrency authority for an enrolled organization.

To repair missing mappings or change routing, explicitly return to preparation:

```shell
cluster-migrator migration set-phase prepare-queues
# Configure missing mappings or adjust routing, then validate again.
cluster-migrator migration set-phase validate-queues
```

Returning to preparation admits unmapped source queues again. It does not move
existing jobs or change concurrency authority. After irreversible commitment,
neither queue-phase command can reopen preparation. Repeated requests for the
current phase return a conflict, not a successful no-op.

Both phase commands require `write_clusters` and permission to change the
organization and manage its destination cluster. Status requires `read_clusters`
and permission to change the organization. These commands use the organization's
fixed destination; they do not accept a destination override.

The server has no read-only readiness endpoint for these transitions, so
`--dry-run` is rejected without a phase-change request. Use `migration status`
to inspect the current phase; status is not authorization for a later change.
`--json` returns the API migration object on success.

Human-readable output includes next steps for the current phase. Preparation
directs you to review mappings before validation. Validation directs you to test
representative workloads and explicitly reopen preparation for repairs. After
commitment, guidance covers concurrency-group progress and coordinating pipeline
moves with Buildkite, not reopening preparation. Status for completed migrations
has no next-step section. JSON output does not include this guidance.

### Request commitment, pipeline migration, and completion

These commands require companion server endpoints and implemented readiness
checks before operational use. The companion server contract rejects otherwise
valid requests with HTTP 503: `Migration transition to <phase> is not available yet`.
Wrong-phase requests return HTTP 409. These are not successful no-ops, and the CLI
does not bypass these server checks or enable enrollment.

When those server checks are implemented and Buildkite authorizes proceeding,
request each transition separately:

```shell
cluster-migrator migration set-phase cutover-concurrency-groups --acknowledge-irreversible
# Inspect current state before requesting any subsequent transition.
cluster-migrator migration status
cluster-migrator migration set-phase migrate-pipelines
cluster-migrator migration status
cluster-migrator migration set-phase complete
```

Concurrency-group commitment freezes queue mappings and routing percentages and
prevents returning to preparation. Without `--acknowledge-irreversible`, the CLI
fails without a transition request and explains these consequences.

Entering `migrate-pipelines` does not move pipelines or finalize builds. Pipeline
moves remain separate operations. Completion does not finalize blocked builds:
all unclustered builds must already be terminal before completion can proceed.
Blocked-build finalization has its own command, described below.

`--acknowledge-irreversible` is a client-side acknowledgement only. It is not sent
to or verified by the server: calling an endpoint requests its action. The server
still enforces phase, readiness, and permissions. Completion accepts no
`--finalize-blocked-builds` flag.

All three commands use the token's organization and its configured migration;
they accept neither a destination override nor a migration UUID. They require
`write_clusters` and permission to change the organization and manage its fixed
destination cluster.

All three POST requests have no body and no `Content-Type` header:

| Command | API operation | Target phase |
| --- | --- | --- |
| `migration set-phase cutover-concurrency-groups --acknowledge-irreversible` | `cutover-concurrency-groups` | `cutover_concurrency_groups` |
| `migration set-phase migrate-pipelines` | `cutover-pipelines` | `migrate_pipelines` |
| `migration set-phase complete` | `complete` | `complete` |

There is no read-only preview endpoint, so all three reject `--dry-run` without
mutation requests, including when the acknowledgement flag is set.
On HTTP 200, human output reports the returned phase and directs you to
`migration status`; `--json` returns only the API migration object. A successful
POST is not proof that all background work has finished. A client timeout does
not imply server-side cancellation; inspect status before deciding what to do
next. Server rejections exit non-zero with empty stdout and the API error on
stderr, including HTTP 404, 409, 422, and 503 responses.

### Finalize blocked builds separately

> [!IMPORTANT]
> This command targets a proposed API endpoint that is not yet implemented.
> Keep it out of operational use until the companion server contract, permissions,
> eligibility rules, and implementation are reviewed and deployed.

During pipeline migration, request finalization separately from completion:

```shell
cluster-migrator migration finalize-blocked-builds
```

The proposed contract is an empty POST to the configured organization's
`unclustered-to-clustered-migration/finalize-blocked-builds` endpoint, returning
the migration object without changing its phase. The CLI never falls back to
`complete`, moves pipelines, or finalizes individual builds itself. The server
must enforce organization and build permissions and finalization eligibility
during `migrate_pipelines`; the CLI is not an authorization boundary.

Human output reports that finalization was requested, not that all builds are
terminal or the migration is complete. `--json` returns the migration object.
There is no read-only preview endpoint, so `--dry-run` exits non-zero without
a finalization request. Missing or rejected endpoints produce a non-zero exit
with empty stdout and the API error on stderr. A timeout does not prove that
the server cancelled the request; inspect affected builds before retrying or
requesting completion.

| Command | Purpose |
| --- | --- |
| `migration configure --destination-cluster <cluster>` | Create the organization's migration with a fixed destination in queue preparation, when server-side enrollment is available. |
| `migration status` | Show the organization's configured migration and phase. |
| `migration set-phase validate-queues` | Reject unmapped source queues and freeze queue configuration without changing concurrency authority. |
| `migration set-phase prepare-queues` | Explicitly return from validation to editable preparation and reopen unmapped-source-queue admission. |
| `migration set-phase cutover-concurrency-groups --acknowledge-irreversible` | Request irreversible commitment, subject to server readiness checks. |
| `migration set-phase migrate-pipelines` | Enter the pipeline-migration phase without moving pipelines or finalizing builds. |
| `migration set-phase complete` | Request completion after all unclustered builds are terminal; does not finalize blocked builds. |
| `migration finalize-blocked-builds` | Request finalization separately without completing the migration; requires the proposed companion server endpoint. |
| `queue configure <queue>` | During preparation, map a source queue to an existing queue in the organization's fixed destination at 0%. |
| `queue status [queue]` | Show one queue migration or list all migrations. |
| `queue set-percent <queue> --to <0-100>` | Set the absolute percentage of new jobs routed to the cluster queue. |
| `queue metrics <queue>` | Compare recent source and destination activity. |
| `concurrency-group cutover <scope>/<key>` | Start holding, draining, and moving a concurrency group into the organization's fixed destination, or report why it can't start. `--dry-run` checks only the group's migration state. |
| `concurrency-group status [<scope>/<key>]` | Show one concurrency-group migration's state and unclustered jobs, or list every migration. A group whose cutover hasn't started has no migration. |
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

All mutations are non-interactive. The CLI reads the organization's phase before queue edits and previews. The server rechecks authoritative state when applying mutations; a successful preview does not reserve the phase or authorize a later edit.

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
