# Cluster Migrator

Cluster Migrator answers one question: **is it safe to permanently move each pipeline from unclustered queues into a cluster?**

> [!IMPORTANT]
> This README defines the mechanism and API contract before implementation. These commands do not exist yet. The [customer runbook](CUSTOMER_RUNBOOK.md) is not production-approved while its blocking decisions remain unresolved.

Migration mode does not move a pipeline immediately. The pipeline remains officially unclustered while Buildkite routes a deterministic percentage of its newly created, non-concurrency jobs into matching cluster queues. An operator can ramp that traffic, observe it, and return new traffic to 0% without restarting the whole fleet.

Queue rollout and pipeline migration are separate decisions:

1. **Scale capacity** in the destination queue.
2. **Roll out queue traffic** to gather evidence that clustered dispatch works: no stranded jobs, acceptable dispatch latency, and working retries, metadata, artifacts, annotations, pipeline uploads, and notifications.
3. **Cut over concurrency groups atomically** so one logical limit is never split across two independent counters.
4. **Check pipeline readiness** for missing destination queues, incomplete queue rollouts, incomplete concurrency-group cutovers, old-scope jobs, and insufficient queue quotas.
5. **Move the pipeline** by setting its permanent `cluster_id` only when the readiness API reports `ready_to_move: true`.
6. **Finalize** only after no pipeline in the organization remains assigned to unclustered.

The workload generator supplies controlled test traffic; it does not prove readiness by itself. Buildkite's migration API owns the authoritative readiness decision and returns actionable blockers.

## One customer-facing CLI

Customers use only `cluster-migrator`. It controls temporary queue routing, reads capacity and readiness from Buildkite, cuts over concurrency groups, and permanently moves ready pipelines. Customers continue to manage agents through their existing fleet tooling.

This repository has two additional local test-harness binaries:

- `agent-scaler` starts and stops local Buildkite agent processes to simulate customer fleet tooling or a production autoscaler such as the Buildkite Elastic CI Stack for AWS.
- `workload-generator` creates probe builds through Buildkite's REST API.

Neither local binary is installed or used by production operators. Neither changes migration state.

The rule is simple: **scale first, cut over second**.

The mock scaler makes capacity changes deterministic for this exercise. It does not reproduce production instance provisioning, autoscaling policies, or failure handling.

## CLI contract

```shell
# The only credentials operators need.
export BUILDKITE_ORGANIZATION_SLUG=<organization>
export BUILDKITE_API_TOKEN=<token>

# Create a migration. Returns the migration UUID.
MIGRATION_UUID=$(cluster-migrator create migration --cluster-uuid <uuid>)

# Inspect migration-wide queues, capacity, traffic, and blockers.
cluster-migrator status --migration-uuid="$MIGRATION_UUID"

# Increase destination capacity through the customer's existing fleet tooling.

# Then route 30% of newly created ordinary jobs to the cluster.
cluster-migrator cutover --migration-uuid="$MIGRATION_UUID" --queue=<queue> --percentage=30 --wait

# Repeat at 60% and 100%, then check migration-wide progress.
cluster-migrator status --migration-uuid="$MIGRATION_UUID"

# Cut over concurrency groups atomically.
cluster-migrator cutover --migration-uuid="$MIGRATION_UUID" --concurrency-group=<group> --wait

# Check whether this pipeline is now safe to move.
cluster-migrator status --migration-uuid="$MIGRATION_UUID" --pipeline=<pipeline-slug>

# Make each ready pipeline's cluster assignment permanent.
cluster-migrator move-pipeline --migration-uuid="$MIGRATION_UUID" --pipeline=<pipeline-slug> --wait

# Guarded cleanup after no pipelines remain assigned to unclustered.
cluster-migrator finalize --migration-uuid="$MIGRATION_UUID"
```

That is the customer workflow. `cluster-migrator` never scales agents or generates builds.

## Operational procedure

[CUSTOMER_RUNBOOK.md](CUSTOMER_RUNBOOK.md) owns the production preconditions, phase gates, emergency actions, rollback boundaries, failure recovery, and audit evidence. It uses placeholders for customer-owned workload and fleet tooling.

[INTERNAL_RUNBOOK.md](INTERNAL_RUNBOOK.md) owns the disposable-organization rehearsal. It uses this repository's `workload-generator` and `agent-scaler` test harnesses.

## What each `cluster-migrator` command does

`cluster-migrator` requires the proposed migration APIs described below, including a migration-owned pipeline move that revalidates readiness atomically. The local harnesses use these existing Buildkite API operations:

```text
agent-scaler:        GET   https://agent.buildkite.com/v3/metrics
workload-generator:  GET   https://api.buildkite.com/v2/organizations/{org}/teams
workload-generator:  GET   https://api.buildkite.com/v2/organizations/{org}/pipelines/{pipeline}
workload-generator:  POST  https://api.buildkite.com/v2/organizations/{org}/pipelines
workload-generator:  POST  https://api.buildkite.com/v2/organizations/{org}/pipelines/{pipeline}/builds
```

The `/cluster-migrations` endpoints below are **proposed API**, not current Buildkite endpoints. `{api}` means `https://api.buildkite.com/v2`. Every `{api}` request sends `Authorization: Bearer $BUILDKITE_API_TOKEN`; repeated examples omit that header for brevity.

### `cluster-migrator create migration`

```shell
cluster-migrator create migration --cluster-uuid=<uuid>
```

Customers should not need agent tokens or an external queue inventory. Buildkite must discover the affected unclustered queue keys, resolve same-key destination queues where possible, and expose missing queues as migration blockers. The current API draft requires a client-supplied `queues` array; this CLI requires that contract to change.

The command creates the server-side migration record:

```http
POST {api}/organizations/{org}/cluster-migrations
Authorization: Bearer $BUILDKITE_API_TOKEN
Content-Type: application/json
Idempotency-Key: <command-uuid>

{
  "destination_cluster_id": "<uuid>"
}
```

The API infers that the source is the organization's unclustered pool, discovers queue mappings, and returns `201 Created` with the migration resource. The command prints only its `id` so the UUID can be captured by the shell. It does not start agents or shift traffic.

### `cluster-migrator cutover --queue`

```shell
cluster-migrator cutover \
  --migration-uuid=<migration-uuid> \
  --queue=<queue> \
  --percentage=<0..100> \
  [--wait]
```

The CLI resolves the queue key to its migration queue resource and reads its ETag:

```http
GET {api}/organizations/{org}/cluster-migrations/{migration}/queues
GET {api}/organizations/{org}/cluster-migrations/{migration}/queues/{migration-queue-id}
```

The queue resource reports source and destination `connected_agents` at `activity.observed_at`. `cluster-migrator` refuses stale data and blocks when Buildkite reports insufficient destination capacity for the requested traffic percentage. Customers change capacity through their existing fleet tooling; the CLI needs no agent tokens. It then requests the absolute traffic target:

```http
PATCH {api}/organizations/{org}/cluster-migrations/{migration}/queues/{migration-queue-id}
Content-Type: application/json
If-Match: <queue-etag>

{"destination_job_percentage":30}
```

The proposed API must revalidate fresh destination capacity atomically with this change; the CLI's preceding read is not a sufficient safety boundary. It returns `200 OK` with the updated queue resource or a structured blocker without changing traffic. Without `--wait`, the command prints that resource. With `--wait`, it polls the queue until `routing.effective_at` confirms the new target and fresh `activity` appears after that time. At 0%, no newly created ordinary jobs route to the cluster; existing clustered jobs stay where they are and drain normally.

- `--percentage` is an absolute clustered-traffic target.
- Traffic cannot exceed fresh, observed clustered capacity.
- Stale or missing capacity blocks the command.
- `--wait` returns after Buildkite applies the target; at 0%, it also confirms that no new ordinary jobs are dispatched to the cluster.
- Retries cannot apply the cutover twice.
- No agent changes.

### `cluster-migrator status`

```shell
cluster-migrator status --migration-uuid=<migration-uuid> [--pipeline=<pipeline-slug>]
```

`status` always needs the migration because the migration identifies the unclustered source, destination cluster, and queue mappings. It needs neither `--unclustered` nor `--cluster-uuid` flags.

Without `--pipeline`, it is an operational overview: all queue rollouts, concurrency-group progress, pipeline summaries, and finalization blockers. It does not claim that every pipeline shares one readiness result. With `--pipeline`, it answers the primary safety question for that pipeline using only its queues, concurrency groups, old-scope jobs, and quotas.

Reads all migration state from the proposed API:

```text
GET {api}/organizations/{org}/cluster-migrations/{migration}
GET {api}/organizations/{org}/cluster-migrations/{migration}/queues
GET {api}/organizations/{org}/cluster-migrations/{migration}/concurrency-groups
GET {api}/organizations/{org}/cluster-migrations/{migration}/pipelines
GET {api}/organizations/{org}/cluster-migrations/{migration}/readiness
```

With `--pipeline`, it additionally reads the pipeline-specific gate:

```text
GET {api}/organizations/{org}/cluster-migrations/{migration}/pipelines/{migration-pipeline-id}/readiness
```

Source and destination capacity come from each queue resource's `activity.connected_agents`. Configured traffic comes from `destination_job_percentage`; observed traffic comes from the same resource's activity window. The pipeline collection and migration readiness response supply `ready_to_move`, `ready_to_complete`, the organization-wide unclustered pipeline count, and structured blockers. The command joins the responses for display but writes nothing and never substitutes local state for missing Buildkite data.

- Reads current state from Buildkite, not previous local commands.
- Shows configured and observed traffic separately.
- Includes observation times and measurement windows.
- Shows blockers for queues, concurrency groups, pipelines, and completion.
- Reports a pipeline safety verdict only when `--pipeline` is supplied.
- Displays stale or missing data as `unknown`, never `0`.

### `cluster-migrator cutover --concurrency-group`

```shell
cluster-migrator cutover \
  --migration-uuid=<migration-uuid> \
  --concurrency-group=<group> \
  --wait
```

Reads the proposed concurrency-group list, resolves the key to its migration group ID, and refuses unless `readiness.ready_to_cut_over` is true and `available_actions` includes `start_cutover`:

```http
GET {api}/organizations/{org}/cluster-migrations/{migration}/concurrency-groups
GET {api}/organizations/{org}/cluster-migrations/{migration}/concurrency-groups/{migration-group-id}
```

It then starts the proposed cutover operation. The CLI converts its default ten-minute timeout into `deadline_at`:

```http
POST {api}/organizations/{org}/cluster-migrations/{migration}/concurrency-groups/{migration-group-id}/cutover
Idempotency-Key: <command-uuid>
Content-Type: application/json

{"deadline_at":"<now-plus-10-minutes>"}
```

The API returns `202 Accepted`. `--wait` polls the concurrency-group resource from `Location` through hold, drain, switch, and release until it succeeds or fails. It never changes queue percentages or agent capacity.

### `cluster-migrator move-pipeline`

```shell
cluster-migrator move-pipeline \
  --migration-uuid=<migration-uuid> \
  --pipeline=<pipeline-slug> \
  --wait
```

First lists affected pipelines, resolves the pipeline slug to its migration pipeline ID, and checks the proposed readiness endpoint:

```http
GET {api}/organizations/{org}/cluster-migrations/{migration}/pipelines
GET {api}/organizations/{org}/cluster-migrations/{migration}/pipelines/{migration-pipeline-id}/readiness
```

The command refuses unless the response has `ready_to_move: true` and `available_actions` includes `move_to_destination_cluster`. The response explains blockers per queue, including missing destination queues or incomplete rollouts; Buildkite also checks concurrency groups, old-scope jobs, and queue quotas. This client-side check is informative, not the safety boundary.

When ready, the command asks the proposed migration API to make the pipeline's cluster assignment permanent:

```http
POST {api}/organizations/{org}/cluster-migrations/{migration}/pipelines/{migration-pipeline-id}/move
Idempotency-Key: <command-uuid>
If-Match: <migration-pipeline-etag>
```

The endpoint must atomically revalidate readiness and reject the move without changing `cluster_id` if any prerequisite changed after the preceding read. With `--wait`, the command polls both the current `GET /organizations/{org}/pipelines/{pipeline}` endpoint and the migration API until `cluster_id` matches and the migration pipeline reports the move complete. This is the actual pipeline cutover; the preceding queue percentages only gathered evidence while the pipeline remained unclustered. The command does not alter the pipeline definition or running builds.

### `cluster-migrator finalize`

```shell
cluster-migrator finalize --migration-uuid=<migration-uuid>
```

Reads the dedicated completion gate and refuses unless `ready_to_complete` is true and `unclustered_pipeline_count` is zero:

```http
GET {api}/organizations/{org}/cluster-migrations/{migration}/readiness
```

It then sends the proposed completion request using the latest migration ETag:

```http
POST {api}/organizations/{org}/cluster-migrations/{migration}/complete
Idempotency-Key: <command-uuid>
If-Match: <migration-etag>
```

The request has no body. The completion endpoint must recheck the organization-wide pipeline invariant atomically; a preceding readiness response can become stale. The API returns `202 Accepted`; `finalize` polls the migration resource from `Location` by default and exits only when the migration reaches a terminal state.

- Refuses to run while any queue, concurrency group, or operation remains unfinished.
- Refuses to run while any organization pipeline is still assigned to unclustered, even if that pipeline was absent from the migration's initial affected-pipeline list.
- Sends the completion request once and waits for the migration to succeed or fail.
- Never changes agent capacity.

## Local test harnesses

Customers do not need these tools. Buildkite can use them in a disposable organization to exercise `cluster-migrator` before the production migration. They are deliberately separate from the customer-facing CLI because production fleet management and workload selection are customer-owned concerns.

### `./agent-scaler.sh cluster`, `unclustered`, and `status`

```shell
./agent-scaler.sh unclustered --queue=<queue> --count=<agents> [--wait]
./agent-scaler.sh unclustered --queue=<queue> --percentage=<0..100> [--wait]
./agent-scaler.sh cluster --queue=<queue> --count=<agents> [--wait]
./agent-scaler.sh cluster --queue=<queue> --percentage=<0..100> [--wait]
./agent-scaler.sh status [--queue=<queue>]
```

Each command selects an agent pool and an absolute target. `--count` sets the target directly. `--percentage` calculates the target from the queue's initial unclustered baseline \(N\):

\[
T = \lceil N \times \text{percentage} / 100 \rceil
\]

Exactly one of `--count` or `--percentage` is required. The first `unclustered --count=N` records \(N\). If a percentage command runs before an unclustered count, the scaler discovers \(N\) from the unclustered Agent Metrics API. Later absolute targets do not redefine this baseline, so repeated percentage commands remain stable.

For example, initialize 10 unclustered agents, then create clustered capacity equal to 30% of that baseline:

```shell
./agent-scaler.sh unclustered --queue=cpu-a --count=10 --wait
./agent-scaler.sh cluster --queue=cpu-a --percentage=30 --wait
```

The clustered command starts three local child processes equivalent to:

```shell
BUILDKITE_AGENT_TOKEN="$BUILDKITE_CLUSTER_AGENT_TOKEN" \
BUILDKITE_BUILD_PATH="<state-dir>/builds/<agent-id>" \
buildkite-agent start \
  --tags "queue=cpu-a,cluster-migrator-owned=true"
```

Changing the clustered target from 30% to 60% starts three more processes, not six. The scaler records the baseline, child PIDs, and build paths under `$XDG_STATE_HOME/agent-scaler`, or `~/.local/state/agent-scaler` when `XDG_STATE_HOME` is unset. Clustered and unclustered process state is separate, so one command never stops agents owned by the other pool.

When reducing either pool, the scaler sends one `SIGTERM` to only its recorded child processes and waits for running jobs to finish; it never sends a force-kill signal. With `--wait`, it polls the selected pool's metrics endpoint until `agents.queues[<queue>].total` equals \(T\). If the observed total does not change for 10 consecutive checks, the scaler restores its locally managed agents to the pre-command count and exits non-zero.

- `cluster` uses the cluster token; `unclustered` uses the unclustered token.
- `--count` is an absolute non-negative agent target.
- `--percentage` is an absolute capacity target relative to the initial unclustered baseline.
- Increasing a target creates agents in only the selected pool.
- Decreasing a target gracefully drains and stops only selected-pool agents previously created by `agent-scaler`.
- `--wait` succeeds after Buildkite observes the target capacity or rolls back after 10 unchanged checks.
- `--unclustered-token` and `--cluster-token` override their environment-variable defaults.
- No traffic changes.

`status` reads each pool's Agent Metrics API once and reports `total`, `idle`, and `busy` agents per queue. It also shows the recorded baseline, live local processes managed by the scaler, and stale PID records without changing local state; local and stale counts use unclustered/clustered order. Omitting `--queue` shows the union of queues observed in both pools and recorded locally.

`agent-scaler` is only a local mock of production fleet tooling such as the Buildkite Elastic CI Stack for AWS. Its operational scope is agent-pool capacity; migration-scoped readiness belongs to `cluster-migrator status`.

### `./workload-generator.sh run`

```shell
./workload-generator.sh run --builds-per-minute=4
```

This tool does not know about migrations. It ensures three pipelines exist, initially creating them as unclustered, then reads each pipeline's default branch using the current Pipelines REST API:

| Pipeline | Definition | Queue routing | Purpose |
| --- | --- | --- | --- |
| `cluster-migrator-target` | `util/workload-generator/.buildkite/target/pipeline.yml` | `target-only` default; selected steps override to `shared` | Pipeline to migrate; includes a concurrency group spanning both queues |
| `cluster-migrator-shared-consumer` | `util/workload-generator/.buildkite/shared-consumer/pipeline.yml` | `shared` default | Proves a queue rollout affects every pipeline using that queue |
| `cluster-migrator-control` | `util/workload-generator/.buildkite/control/pipeline.yml` | `control-only` default | Proves unrelated pipelines and queues remain unaffected |

```http
GET {api}/organizations/{org}/pipelines/{pipeline}
Authorization: Bearer $BUILDKITE_API_TOKEN
```

At `--builds-per-minute=4`, it sends one current Create Build request every 15 seconds, rotating across the three pipelines. The configured rate is total, not per pipeline:

```http
POST {api}/organizations/{org}/pipelines/{pipeline}/builds
Authorization: Bearer $BUILDKITE_API_TOKEN
Content-Type: application/json

{
  "commit": "HEAD",
  "branch": "<pipeline.default_branch>",
  "message": "Cluster migration workload <sequence>"
}
```

The generator creates each pipeline with a dynamic upload step that loads the representative queue-specific job mix from this repository. It prints each response's `web_url`, stops creating builds on `Ctrl-C`, and does not cancel builds already created.

## Authentication

Every executable script loads `.env` from the repository root when it exists. Start from the checked-in template; `.env` is ignored by Git.

```shell
cp .env.example .env
```

Use shell assignment syntax in `.env`. Loaded values are exported to child processes, so `export` is optional. Command-line token flags remain one-off overrides where supported.

Customers need only these values for `cluster-migrator`:

```shell
export BUILDKITE_ORGANIZATION_SLUG=<organization>
export BUILDKITE_API_TOKEN=<token>
```

The API token must have the least-privilege migration and pipeline-read scopes defined by the final API contract. Exact scopes remain a production-blocking decision. `cluster-migrator` never accepts or reads an agent token.

The local harnesses need additional credentials:

```shell
export BUILDKITE_UNCLUSTERED_AGENT_TOKEN=<token>
export BUILDKITE_CLUSTER_AGENT_TOKEN=<token>
```

`workload-generator` uses the organization slug and API token, with `read_teams`, `read_pipelines`, `write_pipelines`, and `write_builds`. `agent-scaler` uses the token for the selected pool. A percentage target also needs the unclustered token when the queue baseline has not been recorded yet.

`agent-scaler` resolves its tokens in this order:

```text
--unclustered-token > BUILDKITE_UNCLUSTERED_AGENT_TOKEN > error
--cluster-token     > BUILDKITE_CLUSTER_AGENT_TOKEN     > error
```

Flags are optional one-off overrides. The environment variables are the defaults and keep tokens out of shell history and process listings. None of the tools prints tokens.

## Safety rules

1. Scale clustered agents before shifting clustered traffic.
2. Keep unclustered agents at 100% until completion.
3. Use absolute targets so every command is retryable.
4. Block traffic changes when capacity is missing or stale.
5. Cut over concurrency groups one at a time.
6. Decrease traffic and receive a dependency-safe confirmation before removing clustered capacity.
7. Use only public Buildkite APIs.
8. Revalidate every irreversible action atomically on the server.

## API requirements

The proposed Buildkite API must expose:

- Migration creation and status.
- At most one active migration for an organization's unclustered source pool, with conflicts returning the existing migration ID.
- Queue traffic targets from 0% to 100%.
- Fresh connected capacity by queue and pool.
- Server-side capacity revalidation before a traffic increase.
- Server-side dependency checks before a traffic decrease so moved pipelines and cut-over concurrency groups cannot be stranded.
- Observed traffic over a named interval.
- Concurrency-group readiness and cutover state.
- Defined timeout, cancellation, and fail-safe release behavior for concurrency-group cutovers.
- Pipeline readiness and migration blockers.
- A migration-owned pipeline move that atomically revalidates readiness and resource versions.
- Completion readiness, including the count and identities of every pipeline still assigned to unclustered.
- Server-side enforcement that the unclustered pipeline count is zero when completion begins.
- Idempotency keys and resource versions for mutations.
- `202 Accepted`, `Location`, and `Retry-After` for long-running operations.
- Operation state that another operator can reattach to after an interrupted wait.
- Continuous discovery of pipeline and queue topology changes while a migration is active.
- Least-privilege authorization and actor-attributed audit events for every mutation.

These endpoints do not exist yet. The tools must not replace missing Buildkite state with local guesses.

## Definition of done

An operator can follow [INTERNAL_RUNBOOK.md](INTERNAL_RUNBOOK.md) against a disposable organization and:

- Create one migration.
- Add 30%, 60%, and 100% clustered capacity without shifting traffic.
- Cut over queue traffic to the same percentages only after agents connect.
- See configured and observed traffic separately.
- Return reversible queue traffic to 0%.
- Cut over concurrency groups one at a time.
- Move ready pipelines, prove that no pipelines remain assigned to unclustered, and finalize the migration.
- Retry every command without duplicating work.
- Finish without exposing secrets.

## Production-blocking decisions

1. Which proposed migration API response supplies observed traffic?
2. How many jobs and how much time make a traffic percentage stable?
3. Which queue-pressure signals should block the next phase?
4. When do concurrency-group or pipeline moves make queue rollback irreversible?
5. What exact capacity formula gates traffic increases and decreases?
6. What happens to held jobs when concurrency-group cutover times out, fails, or is cancelled?
7. Is a moved pipeline reversible? If not, what is the supported emergency response?
8. What are the exact token scopes, audit events, timeout behavior, and exit codes?

## References

- [Draft external API](https://app.notion.com/p/3abb8dbc2c89800ca013eea5cc5843b3)
- [Migration-mode mockups](https://slopcannon.tail952194.ts.net/chris/migration-mode-mockups/)
