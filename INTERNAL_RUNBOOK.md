# Internal Cluster Migration Demo Runbook

This runbook exercises the migration flow in a disposable Buildkite organization with this repository's workload generator and local agent scaler. It is for Buildkite testing only. Customers follow [CUSTOMER_RUNBOOK.md](CUSTOMER_RUNBOOK.md) and manage workloads and capacity with their own tooling.

Use the preconditions, phase gates, emergency actions, and evidence requirements from the customer runbook. The commands below replace only the customer-owned workload and fleet-management steps.

## Configure the environment

Copy the checked-in example and populate the setup inputs:

```shell
cp .env.example .env
```

| Variable | Value |
| --- | --- |
| `BUILDKITE_ORGANIZATION_SLUG` | Disposable organization slug |
| `BUILDKITE_API_TOKEN` | API token created above |
| `BUILDKITE_API_URL` | API base URL for the environment under test |
| `BUILDKITE_GRAPHQL_URL` | GraphQL API URL for the environment under test |
| `BUILDKITE_AGENT_ENDPOINT` | Agent API endpoint for the environment under test |

The organization must be disposable, have legacy **Unclustered mode** enabled, and have an `Everyone` team. Keep it in Unclustered mode until every demo pipeline has moved and finalization has passed. The API token needs `read_clusters`, `write_clusters`, `read_teams`, `read_pipelines`, `write_pipelines`, and `write_builds`, the migration scopes required by the API under test, and GraphQL API access.

The top-level scripts load `.env` automatically. Keep `.env` out of commits; it is already ignored by Git.

## Prepare the test organization

Run:

```shell
export BUILDKITE_API_URL="https://api.buildkite.localhost/v2"
export BUILDKITE_GRAPHQL_URL="https://graphql.buildkite.localhost/v1"
export BUILDKITE_AGENT_ENDPOINT="https://agent.buildkite.localhost/v3"
export BUILDKITE_API_TOKEN=<token>
./setup-organization.sh
```

Setup configures the demo pipelines to build `main`. To rehearse an unmerged
change, pass its branch and a run-specific resource prefix:

```shell
./setup-organization.sh \
  --branch=mitch/realistic-migration-data \
  --prefix=cluster-migrator
```

Setup creates the complete queue inventory, records the prefix in `.env`, and
fails rather than silently reusing a pipeline with an incompatible branch,
repository, cluster assignment, bootstrap queue, or upload command.

## Create the migration

Create the migration once and capture its UUID:

```shell
source .env
MIGRATION_UUID=$(cluster-migrator create migration \
  --cluster-uuid="$DESTINATION_CLUSTER_UUID")
export MIGRATION_UUID
```

## Create source capacity

Start source agents for every active queue in the canonical topology. Leave the
rare queue dormant until its focused rehearsal:

```shell
source .env
PREFIX=${MIGRATION_WORKLOAD_PREFIX:-cluster-migrator}
ACTIVE_QUEUES=(
  target-only shared peer-only private-a private-b
  dynamic-bootstrap dynamic-blue dynamic-green control-only
)

for suffix in "${ACTIVE_QUEUES[@]}"; do
  ./agent-scaler.sh unclustered --queue="$PREFIX-$suffix" --count=3 --wait
done
./agent-scaler.sh status
```

Do not start the workload if any active queue lacks source capacity. Keep the
source agents running until migration completion.

## Run the canonical workload

First run one bounded smoke cycle, then start steady traffic in another terminal:

```shell
./workload-generator.sh run --load=smoke --cycles=1
./workload-generator.sh run --load=steady --continuous
```

All four load profiles exercise the same six-pipeline topology. They differ only
in rate and job duration; do not treat them as different functional scenarios.
The `pressure` profile intentionally grows backlog behind serialized groups and
must run for a bounded observation window unless sustained saturation is wanted.

| Relationship under test | Fixture evidence |
| --- | --- |
| One pipeline to one queue | `control` → `control-only` |
| One pipeline to several private queues | `isolated-multi` → `private-a`, `private-b` |
| Shared and overlapping queues | `target`, `shared-consumer`, and `group-peer` share or overlap `shared` and `peer-only` |
| Local and cross-pipeline concurrency | Namespaced static groups span queues and pipelines |
| Dynamic topology | Stable bootstrap; blue/green execution; per-build and branch groups; delayed upload |
| Portable CI behavior | Logs, metadata, artifact transfer, dependency, annotation, and deterministic retry |
| Matched control | `control` remains outside the migrated relationships |

Verification is separate from generation. For each phase, retain Buildkite build
URLs and confirm job queue, dispatch scope, dependency result, artifact download,
metadata read, annotation, retry count, and concurrency behavior in Buildkite.
The generator's successful API response proves only that it created a build.

## Rehearse the capacity gate intentionally

Insufficient capacity is an operational precondition to create with
`agent-scaler`, not a workload-generator fault mode. Use one active queue:

1. Keep steady demand and healthy source capacity on `$PREFIX-target-only`.
2. Set destination capacity to zero.
3. Attempt a traffic increase and verify the API rejects it with a capacity blocker.
4. Set destination capacity below the requested threshold and verify the blocker remains.
5. Scale to sufficient capacity with `--wait`, obtain fresh telemetry, and verify the action becomes available.
6. Increase traffic and confirm observed dispatch follows the configured target.
7. In the disposable environment, age or pause the capacity telemetry past its staleness limit; verify another increase blocks, then restore fresh telemetry and verify recovery.

Example capacity controls:

```shell
./agent-scaler.sh cluster --queue="$PREFIX-target-only" --count=0 --wait
cluster-migrator cutover --migration-uuid="$MIGRATION_UUID" \
  --queue="$PREFIX-target-only" --percentage=30 --wait # expected to fail

./agent-scaler.sh cluster --queue="$PREFIX-target-only" --percentage=30 --wait
cluster-migrator cutover --migration-uuid="$MIGRATION_UUID" \
  --queue="$PREFIX-target-only" --percentage=30 --wait
```

## Exercise the topology in migration order

Use the normal 30%, 60%, and 100% queue rollout gates from the customer runbook,
but order the rehearsal to expose relationship-specific blockers:

1. **Private queues:** roll out `private-a` while `private-b` remains at 0%. Verify only `isolated-multi` remains blocked, then complete `private-b`.
2. **Shared queues:** roll out `shared` and verify the impact across `target`, `shared-consumer`, and `group-peer`; `control` must remain unchanged.
3. **Overlapping queue:** roll out `peer-only` and verify both consumers before either affected pipeline is moved.
4. **Concurrency:** cut over `$PREFIX/local/target` independently from `$PREFIX/shared/cross-pipeline`. Verify the latter covers all three participant pipelines atomically.
5. **Dynamic graph:** roll out `dynamic-bootstrap`, `dynamic-blue`, and `dynamic-green`. Verify per-build groups remain isolated, branch groups are reused, and delayed uploads join the existing build-level group.
6. **Late queue:** after readiness is otherwise green, start source capacity and deliberately expose the dormant queue:

   ```shell
   ./agent-scaler.sh unclustered --queue="$PREFIX-dynamic-rare" --count=1 --wait
   ./workload-generator.sh trigger --workload=late-queue
   cluster-migrator status --migration-uuid="$MIGRATION_UUID"
   ```

   Verify the newly observed queue invalidates readiness until its destination
   capacity, rollout, and dynamic concurrency dependencies satisfy the normal gates.
7. **Pipeline moves:** move only pipelines whose own readiness is green. Verify the matched control throughout, then complete the remaining pipeline and finalization gates.

At every step, capture migration status and apply the customer runbook's soak,
health, sample-size, rollback, and approval requirements. The generator must not
encode this sequence.

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
