# Internal Cluster Migration Demo Runbook

This runbook exercises the migration flow in a disposable Buildkite organization with this repository's workload generator and local agent scaler. It is for Buildkite testing only. Customers follow [CUSTOMER_RUNBOOK.md](CUSTOMER_RUNBOOK.md) and manage workloads and capacity with their own tooling.

Use the preconditions, phase gates, emergency actions, and evidence requirements from the customer runbook. The commands below replace only the customer-owned workload and fleet-management steps.

## Prepare the demo

Set the migration and local harness credentials:

```shell
export BUILDKITE_ORGANIZATION_SLUG=<organization>
export BUILDKITE_API_TOKEN=<token>
export BUILDKITE_UNCLUSTERED_AGENT_TOKEN=<token>
export BUILDKITE_CLUSTER_AGENT_TOKEN=<token>
export DESTINATION_CLUSTER_UUID=<uuid>
```

Create the migration once and capture its UUID:

```shell
MIGRATION_UUID=$(cluster-migrator create migration \
  --cluster-uuid="$DESTINATION_CLUSTER_UUID")
export MIGRATION_UUID
```

## Start representative workloads

In another terminal, run:

```shell
./workload-generator.sh run --builds-per-minute=4
```

The generator creates continuous traffic for:

| Pipeline | Queue coverage | Purpose |
| --- | --- | --- |
| `cluster-migrator-target` | `target-only` and `shared` | Pipeline to migrate |
| `cluster-migrator-shared-consumer` | `shared` | Confirms a queue rollout affects all consumers |
| `cluster-migrator-control` | `control-only` | Confirms unrelated traffic is unaffected |

Move the target pipeline only after both `target-only` and `shared` are ready. Confirm the shared consumer observes the `shared` rollout and the control pipeline remains unaffected.

## Create source capacity

Start representative unclustered agents for every demo queue:

```shell
./agent-scaler.sh unclustered --queue=target-only --count=<agents> --wait
./agent-scaler.sh unclustered --queue=shared --count=<agents> --wait
./agent-scaler.sh unclustered --queue=control-only --count=<agents> --wait
```

Keep this source capacity unchanged until the migration is complete.

## Roll out each queue

For each queue, create destination capacity before setting the matching traffic target:

```shell
./agent-scaler.sh cluster --queue=<queue> --percentage=30 --wait
cluster-migrator cutover --migration-uuid="$MIGRATION_UUID" --queue=<queue> --percentage=30 --wait

./agent-scaler.sh cluster --queue=<queue> --percentage=60 --wait
cluster-migrator cutover --migration-uuid="$MIGRATION_UUID" --queue=<queue> --percentage=60 --wait

./agent-scaler.sh cluster --queue=<queue> --percentage=100 --wait
cluster-migrator cutover --migration-uuid="$MIGRATION_UUID" --queue=<queue> --percentage=100 --wait
```

At every percentage, run migration status and apply the customer runbook's soak, health, sample-size, and approval gates before proceeding:

```shell
cluster-migrator status --migration-uuid="$MIGRATION_UUID"
```

After all queues pass at 100%, exercise the concurrency-group cutovers, pipeline moves, finalization, and evidence capture in [CUSTOMER_RUNBOOK.md](CUSTOMER_RUNBOOK.md).

## Failure-injection rehearsal

Before production approval, deliberately verify:

- Stale and missing capacity block increases.
- A missing destination queue appears as a blocker.
- Repeating absolute targets does not duplicate work.
- A resource-version conflict fails closed.
- Interrupting `--wait` leaves an observable operation another operator can reattach to.
- Concurrency-group timeout or cancellation releases held jobs into the documented scope.
- Returning reversible ordinary traffic to 0% prevents new destination dispatch before capacity is reduced.
- Traffic decreases are blocked after a moved dependency makes them unsafe.
- A pipeline readiness change racing with a move is rejected atomically.
- A second migration for the same source pool is rejected with the active migration ID.
- Finalization is rejected while any organization pipeline remains unclustered.

## Clean up

Stop `workload-generator.sh` with `Ctrl-C`. This stops creating builds but does not cancel existing builds.

Clean up locally managed agents only after traffic and dependency checks prove removal is safe, or after the disposable organization and its workloads have been disabled.
