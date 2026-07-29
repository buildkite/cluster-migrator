# Cluster Migration Runbook

This runbook moves Buildkite pipelines from unclustered queues into a destination cluster. [README.md](README.md) defines the mechanism and API contract; this document defines the operator procedure.

> [!CAUTION]
> This runbook is not production-approved. The CLI and proposed migration APIs are not implemented, and every item in **Production approval gate** must be checked before a production migration. Do not replace unresolved contracts with operator judgment.

## Production approval gate

The approver must confirm all items before scheduling production work:

- [ ] `cluster-migrator` is released, its version is pinned below, and every command in this runbook has been exercised in a disposable organization.
- [ ] Buildkite enforces at most one active migration for the organization's unclustered source pool.
- [ ] Pipeline moves use a migration-owned endpoint that atomically revalidates readiness and resource versions.
- [ ] Traffic decreases are rejected when moved pipelines or cut-over concurrency groups still depend on destination capacity.
- [ ] Concurrency-group timeout, cancellation, held-job release, and terminal-state behavior are implemented and tested.
- [ ] Interrupted `--wait` operations can be found and observed by another operator.
- [ ] Minimum soak time, minimum job count, traffic tolerance, latency limits, and queue-pressure limits are filled in below.
- [ ] Exact API token scopes and actor-attributed audit events are documented and verified.
- [ ] The supported response to a bad pipeline move is documented under **Emergency pipeline response**.
- [ ] Buildkite has supplied evidence that every required failure-injection rehearsal passed in a disposable organization.

Approval record:

| Field | Value |
| --- | --- |
| Approver | `<name>` |
| Approval time | `<UTC timestamp>` |
| Rehearsal migration | `<migration UUID and evidence link>` |
| `cluster-migrator` version | `<version or commit>` |
| Buildkite API version | `<version>` |

## Ownership and coordination

Use one driver for every mutation. Other operators may run `status`, but must not change migration state or fleet capacity without a handoff recorded in the incident or change channel.

| Role or record | Value |
| --- | --- |
| Driver | `<name>` |
| Customer approver | `<name>` |
| Customer fleet owner | `<name>` |
| Buildkite migration owner | `<name>` |
| Buildkite escalation | `<channel or paging route>` |
| Change channel | `<channel>` |
| Change window | `<start and end in UTC>` |
| Organization | `<slug>` |
| Destination cluster | `<name and UUID>` |
| Migration UUID | `<set after creation>` |

The driver records every mutation, timestamp, result, approval, and associated status output in the change record.

## Emergency stop

Use this procedure when impact is suspected or migration state is uncertain.

1. Announce a migration hold in the change channel. Stop all new migration mutations and fleet reductions.
2. Preserve source and destination capacity. Do not terminate agents.
3. Capture authoritative state:

   ```shell
   cluster-migrator status --migration-uuid="$MIGRATION_UUID"
   ```

4. For each queue, inspect blockers and available actions. Return ordinary new-job traffic to 0% only when Buildkite reports that the decrease is safe:

   ```shell
   cluster-migrator cutover \
     --migration-uuid="$MIGRATION_UUID" \
     --queue=<queue> \
     --percentage=0 \
     --wait
   ```

5. If the decrease is blocked, a concurrency-group operation is active, or any affected pipeline has moved, preserve destination capacity and page the Buildkite migration owner. Do not override the blocker.
6. Re-run `status`. Confirm the configured target is 0%, its effective time has passed, and fresh activity shows no new ordinary jobs dispatching to the destination.
7. Reduce destination capacity only after Buildkite reports no remaining dependency and in-flight destination jobs have drained.

Returning ordinary traffic to 0% does not move completed concurrency groups, reverse moved pipelines, relocate in-flight jobs, or cancel a server-side operation. Never assume `Ctrl-C` cancelled an operation; treat its outcome as unknown until `status` proves otherwise.

## Preconditions

### Destination and fleet

- [ ] The destination cluster exists.
- [ ] Every source queue key has a matching destination queue, or its absence is an intentional blocker.
- [ ] Customer fleet tooling has a valid destination-cluster agent token for each queue.
- [ ] Destination agents have the required hooks, secrets, plugins, permissions, network access, and artifact configuration.
- [ ] Source capacity will remain at 100% until the migration is complete.
- [ ] The fleet owner has documented the exact capacity increase and decrease commands outside this repository.

### Access and control

- [ ] `BUILDKITE_API_TOKEN` has only the approved migration and pipeline scopes.
- [ ] The driver can read migration audit events.
- [ ] No other active migration exists for the organization's unclustered source pool.
- [ ] No conflicting queue, pipeline, concurrency, or fleet change is scheduled during the window.
- [ ] Buildkite and customer escalation owners are available for the entire window.

### Baseline health

- [ ] Source dispatch latency, queue depth, job failure rate, and agent connectivity are within their normal ranges.
- [ ] Destination capacity telemetry is fresh.
- [ ] Representative pipelines cover ordinary jobs, long-running jobs, retries, concurrency groups, metadata, artifacts, annotations, pipeline uploads, and notifications.
- [ ] Known pre-existing failures are recorded so they are not attributed to the migration.
- [ ] Emergency stop and bad-pipeline response have been reviewed by the driver and approver.

## Phase gates

These values must be approved before production. A blank value blocks the migration.

| Gate | Approved value |
| --- | --- |
| Minimum soak at each percentage | `<duration>` |
| Minimum destination-dispatched jobs per queue | `<count>` |
| Configured versus observed traffic tolerance | `<percentage points>` |
| Maximum dispatch-latency regression | `<value>` |
| Maximum destination queue wait | `<value>` |
| Maximum job failure-rate regression | `<value>` |
| Maximum retry-rate regression | `<value>` |
| Capacity headroom required before increase | `<value and formula>` |
| Maximum telemetry age | `<duration>` |

At every gate, stop rather than proceed when data is stale, missing, below the minimum sample, or outside an approved limit.

## Prepare the shell

Use a shell that will remain available for the change. Do not place tokens in command arguments.

```shell
export BUILDKITE_ORGANIZATION_SLUG=<organization>
export BUILDKITE_API_TOKEN=<token>
export DESTINATION_CLUSTER_UUID=<uuid>
```

Record the CLI version and verify the intended organization and destination cluster before creating state.

## Create and inspect the migration

Create the migration once and capture its UUID:

```shell
MIGRATION_UUID=$(cluster-migrator create migration \
  --cluster-uuid="$DESTINATION_CLUSTER_UUID")
export MIGRATION_UUID
printf '%s\n' "$MIGRATION_UUID"
```

If creation reports an existing active migration, stop and inspect that migration. Do not create another one.

Capture the baseline:

```shell
cluster-migrator status --migration-uuid="$MIGRATION_UUID"
```

Do not continue unless:

- The organization and destination cluster are correct.
- Every expected source queue and affected pipeline is present.
- Configured destination traffic is 0% for every queue.
- Source capacity is healthy and destination capacity is reported accurately.
- Missing queues, stale telemetry, unknown values, and other blockers are understood.
- Creating the migration did not change traffic, capacity, concurrency groups, or pipeline assignments.

Save the output in the change record.

## Start representative workloads

Start customer-selected builds that exercise every affected queue and the functional checks listed under **Baseline health**:

```shell
# Start your representative workloads here using your normal build-triggering process.
# Keep them running throughout the queue rollout and pipeline verification.
```

Record the pipeline and build links used as migration evidence.

## Roll out ordinary queue traffic

Process one queue at a time through 30%, 60%, and 100%. Percentages are absolute targets. Keep source capacity unchanged.

### Increase destination capacity

Before each traffic increase, set destination capacity to the approved target using your existing fleet tooling:

```shell
# Scale your destination infrastructure here using your normal fleet tooling.

# Then check that Buildkite reports fresh destination capacity for the queue.
curl --fail --silent --show-error \
  --header "Authorization: Bearer $BUILDKITE_API_TOKEN" \
  "https://api.buildkite.com/v2/organizations/$BUILDKITE_ORGANIZATION_SLUG/cluster-migrations/$MIGRATION_UUID/queues"
```

Do not continue until the queue resource reports sufficient destination `connected_agents` and a fresh `activity.observed_at` for the requested traffic.

### Set the traffic target

```shell
cluster-migrator cutover \
  --migration-uuid="$MIGRATION_UUID" \
  --queue=<queue> \
  --percentage=<30|60|100> \
  --wait
```

If the command is interrupted or fails, go to **Failure playbook**. Do not issue a conflicting target until `status` establishes the current effective target and operation state.

### Verify the phase

```shell
cluster-migrator status --migration-uuid="$MIGRATION_UUID"
```

Start the soak only after `routing.effective_at` has passed and fresh activity was observed after that time. Continue only when all checks pass:

- [ ] Configured traffic equals the requested target.
- [ ] Observed traffic is within the approved tolerance over the named window.
- [ ] The approved minimum soak and job count have both been reached.
- [ ] Destination capacity and headroom remain above the approved minimum.
- [ ] Dispatch latency, queue wait, failure rate, and retry rate remain within limits.
- [ ] No job is stranded or unexpectedly routed.
- [ ] Representative destination jobs pass metadata, artifacts, annotations, pipeline uploads, and notifications checks.
- [ ] Long-running and retried jobs behave as expected.
- [ ] The driver saved evidence and the approver authorized the next target.

Repeat for 30%, 60%, and 100%, then repeat for the next queue. A failed gate triggers **Emergency stop** or a hold for investigation; it never triggers an automatic increase.

## Cut over concurrency groups

Cut over one group at a time only after its readiness endpoint allows `start_cutover` and all queues it can request have passed their 100% gate.

```shell
cluster-migrator cutover \
  --migration-uuid="$MIGRATION_UUID" \
  --concurrency-group=<group> \
  --wait
```

During the operation, monitor hold, drain, switch, and release state. Do not begin another group until the current group reaches a terminal state and held jobs have been released.

After success, verify:

- The group is assigned to the destination scope.
- No source-scope jobs remain for the group.
- Held jobs were released exactly once.
- The configured concurrency limit was never split or exceeded.
- Queue latency and job failures remain within approved limits.

On timeout, failure, or interruption, preserve both source and destination capacity and follow **Failure playbook**. Do not retry until Buildkite reports the group's terminal scope and held-job disposition.

## Move pipelines

Treat this step as irreversible until **Emergency pipeline response** contains an approved reversal procedure.

For each pipeline, capture its readiness:

```shell
cluster-migrator status \
  --migration-uuid="$MIGRATION_UUID" \
  --pipeline=<pipeline-slug>
```

Do not continue unless Buildkite reports `ready_to_move: true`, exposes `move_to_destination_cluster`, and lists no blockers. Obtain the recorded approval, then move the pipeline:

```shell
cluster-migrator move-pipeline \
  --migration-uuid="$MIGRATION_UUID" \
  --pipeline=<pipeline-slug> \
  --wait
```

The server must revalidate readiness atomically with this action. After success:

- Verify the pipeline's permanent `cluster_id` matches the destination.
- Verify the migration API reports the pipeline moved.
- Run a representative build and verify dispatch, retries, metadata, artifacts, annotations, uploads, and notifications.
- Confirm no old-scope jobs remain and queue health stays within limits.
- Save the pre-move readiness, mutation result, post-move state, and build links.

Move one pipeline at a time. Stop on the first failed post-move check.

## Emergency pipeline response

No supported reversal is defined yet. Until Buildkite and the customer approve one:

1. Stop migration mutations and destination capacity reductions.
2. Preserve enough destination capacity for moved pipelines and concurrency groups.
3. Capture migration-wide and pipeline-specific status.
4. Page the customer fleet owner and Buildkite migration owner.
5. Do not directly patch `cluster_id` or improvise a move back to unclustered.

Production approval requires either a tested reversal procedure with exact consequences for in-flight jobs and concurrency groups, or explicit acceptance that escalation and destination-capacity preservation are the only supported response.

## Finalize

Capture migration-wide status:

```shell
cluster-migrator status --migration-uuid="$MIGRATION_UUID"
```

Do not finalize unless:

- Every queue rollout and concurrency-group operation is complete.
- Every affected pipeline has moved.
- `unclustered_pipeline_count` is zero across the organization.
- `ready_to_complete` is true and there are no blockers.
- No migration operation is active.
- Post-move verification has passed for every pipeline.

Finalize:

```shell
cluster-migrator finalize --migration-uuid="$MIGRATION_UUID"
```

Wait for a terminal migration state. Save the final status and audit events. Finalization does not change fleet capacity; the fleet owner handles later capacity normalization as a separate approved change.

## Reversibility matrix

| Current state | Supported operator action | Prohibited action |
| --- | --- | --- |
| Queue at 0%, no moved dependencies | Keep or increase destination capacity; begin a guarded ramp | Remove source capacity |
| Queue at 30%, 60%, or 100%; no group or pipeline moved | Return ordinary new-job traffic to 0%, wait for confirmation, then drain destination jobs | Reduce destination capacity before traffic is 0% and jobs drain |
| Concurrency-group cutover active | Observe status and escalate on timeout or failure | Assume interruption cancelled it; start a conflicting cutover |
| Concurrency group moved | Preserve destination capacity; decrease ordinary traffic only when the API reports it safe | Attempt to move the group back without an approved procedure |
| Pipeline moved | Preserve destination capacity and use **Emergency pipeline response** | Route away or remove capacity that the pipeline requires; patch `cluster_id` directly |
| Migration finalized | Operate the destination cluster through normal fleet procedures | Use this runbook as a rollback mechanism |

## Failure playbook

| Symptom | Safe response |
| --- | --- |
| Missing or stale capacity | Hold. Restore telemetry or capacity, then re-run `status`. Never interpret unknown as zero. |
| Missing destination queue | Hold. Have the cluster owner create or approve the queue, then re-run discovery and readiness. |
| Capacity blocker | Do not override it. Increase healthy destination capacity or lower the requested traffic target. |
| Resource-version conflict | Another actor changed state. Re-run `status`, coordinate ownership, and decide from current state. |
| `--wait` interrupted | Treat the mutation outcome as unknown. Re-run `status` and reattach to the server-side operation before any new mutation. |
| Queue target fails to become effective | Preserve capacity, capture operation and activity timestamps, and page Buildkite. Do not repeat blindly. |
| Observed traffic is missing or outside tolerance | Hold at the current target. Investigate routing and sample size; do not advance. |
| Concurrency-group deadline or failure | Preserve both capacities. Determine terminal scope and held-job disposition through `status`; page Buildkite before retrying. |
| Pipeline move returns an error | Query both pipeline `cluster_id` and migration state. Do not assume the pipeline remained on the source. |
| Post-move functional check fails | Use **Emergency pipeline response**. Do not move another pipeline. |
| Traffic decrease is blocked | Preserve destination capacity. Identify moved pipeline or concurrency dependencies; do not override. |
| Finalization fails | Preserve all capacity and migration state. Re-run readiness and resolve blockers; do not perform cleanup. |
| Driver becomes unavailable | Record a handoff. The new driver starts by running `status` and reviewing audit events; they do not trust shell-local state. |

## Post-migration verification

- [ ] Migration state is terminal and successful.
- [ ] Every organization pipeline has the intended destination `cluster_id`.
- [ ] No unclustered jobs, held concurrency jobs, or active migration operations remain.
- [ ] Queue depth, dispatch latency, job failures, retries, and agent connectivity are healthy through the approved post-migration observation window.
- [ ] Representative builds pass all functional checks.
- [ ] Audit events identify every mutation and actor.
- [ ] The change record contains versions, commands, timestamps, status output, approvals, metrics, and build links.
- [ ] Follow-up fleet normalization is tracked separately.
