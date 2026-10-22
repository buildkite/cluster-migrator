# Cluster Migrator

`cluster-migrator` moves an organization's unclustered workloads to one fixed destination cluster. It gradually routes queue traffic, validates mappings, commits bulk concurrency-group cutover, authorizes bulk pipeline cutover, and explicitly completes the migration.

> [!IMPORTANT]
> The organization migration APIs are under development and are not deployed. Enrollment is intentionally disabled on the server. Do not use this CLI for a production migration until Buildkite confirms the APIs and safety prerequisites are available. Local client tests use fixtures; they do not establish server readiness.

## How it works

The server stores the destination and controls five phases:

1. **`prepare_queues`:** map source queues to existing destination queues and increase routing gradually. Routing remains reversible, including after reaching 100%.
2. **`validate_queues`:** exercise normal workloads with queue validation enabled. Return explicitly to preparation before repairing mappings or percentages. Validation does not authorize cutover.
3. **`cutover_concurrency_groups`:** explicitly acknowledge irreversible commitment. This freezes mappings and percentages and prevents returning to preparation. The server discovers, holds, drains, and cuts over concurrency groups in bulk.
4. **`migrate_pipelines`:** the server enters this phase automatically after concurrency-group cutover genuinely finishes. Entering it does not authorize pipeline movement. The operator separately authorizes bulk pipeline cutover.
5. **`complete`:** explicitly request completion after the server's authoritative checks pass. Finishing pipeline movement alone does not complete the migration.

Bulk migration commands are the supported cutover workflow. Operators do not need to enumerate concurrency groups or pipelines. Queue status, queue metrics, and concurrency-group status remain diagnostic tools.

Earlier `concurrency-group cutover`, `pipeline readiness`, and `pipeline move` commands are removed without aliases. There is no `pipeline cutover` command. `queue configure` no longer accepts `--destination-cluster`; configure the organization destination once with `migration configure`.

Routing changes affect new jobs only. Existing jobs stay on the queue selected when they were created. After commitment, recovery is forward-only: do not add mappings, change percentages, or create queues as an automatic recovery step.

## Prerequisites

- Ask Buildkite to confirm migration enrollment and the required APIs are available.
- Enable Advanced Queue Metrics to compare source and destination activity.
- Create the destination cluster and its queues. Each destination queue must have the same exact, case-sensitive key as its source queue. The CLI never creates queues.
- Prepare enough destination agent capacity for the traffic you will route.
- Create an API token that can access exactly one organization. The workflow needs `read_organizations`, `read_clusters`, and `write_clusters`; pipeline API access may also require `read_pipelines` and `write_pipelines`. The user needs permission to change the organization and manage the destination cluster. Server permissions remain authoritative.

```shell
export BUILDKITE_API_TOKEN=<token>
```

The CLI discovers the organization from the token and caches its slug. It refreshes stale organization-cache entries automatically. It does not select or cache a migration locally. No command takes a migration UUID.

## Install

Install the latest release with [mise](https://mise.jdx.dev/):

```shell
mise use --global github:buildkite/cluster-migrator
```

Alternatively, download a binary from [GitHub Releases](https://github.com/buildkite/cluster-migrator/releases). Releases support macOS and Linux on x86-64 and ARM64.

## Migrate an organization

### 1. Configure the fixed destination

Use a cluster name or ID. Previewing configuration checks the server's authorization, destination, and enrollment boundary without creating a migration:

```shell
cluster-migrator migration configure --destination-cluster production --dry-run
cluster-migrator migration configure --destination-cluster production
cluster-migrator migration status
```

The destination cannot be changed after configuration. A same-destination retry preserves the existing migration and phase. Enrollment currently returns an unavailable error even for otherwise valid configuration requests.

### 2. Prepare queue mappings and routing

While the phase is `prepare_queues`, map each source queue to its existing destination queue and ramp routing:

```shell
cluster-migrator queue configure test
cluster-migrator queue set-percent test --to 10
cluster-migrator queue metrics test
cluster-migrator queue set-percent test --to 30
cluster-migrator queue metrics test
cluster-migrator queue set-percent test --to 100
```

Percentages are absolute. Before increasing routing, check connected agents, waiting jobs, p95 wait time, and your normal dispatch and stranded-job observability. Metrics provide context, not proof that an increase is safe. Infrequent queues need representative workloads before you continue.

Before commitment, returning to preparation permits reducing routing even from 100%:

```shell
cluster-migrator queue set-percent test --to 0
```

This does not move existing jobs or remove the mapping. Reaching 100% does not authorize cutover or transfer new-group concurrency authority.

### 3. Validate and repair

```shell
cluster-migrator migration validate-queues --dry-run
cluster-migrator migration validate-queues
cluster-migrator migration status
```

Exercise static and dynamically generated pipeline steps. Validation rejects unmapped source queues. It does not require every mapping to be at 100%.

If you need to repair mappings or percentages, explicitly reopen preparation first:

```shell
cluster-migrator migration prepare-queues
cluster-migrator queue configure release
cluster-migrator migration validate-queues
```

### 4. Commit bulk concurrency-group cutover

```shell
cluster-migrator migration cutover-concurrency-groups --dry-run
cluster-migrator migration cutover-concurrency-groups --acknowledge-irreversible
cluster-migrator migration status
```

The acknowledgement reaches the server. Commitment freezes configuration and is irreversible. The server checks current readiness before accepting the operation; a cached preview cannot authorize it.

An accepted response means background work was authorized, not that cutover finished. Completion of all required concurrency-group work automatically advances the phase to `migrate_pipelines`, but does not authorize pipeline movement.

For diagnosis, groups use `<scope>/<key>`: `custom/<key>` for YAML concurrency groups and `step/<id>` for groups created for a step. Equal keys in different scopes are distinct; keys may contain slashes.

```shell
cluster-migrator concurrency-group status
cluster-migrator concurrency-group status custom/deploy/production
```

### 5. Authorize bulk pipeline cutover

```shell
cluster-migrator migration status
cluster-migrator migration cutover-pipelines --dry-run
cluster-migrator migration cutover-pipelines
cluster-migrator migration status
```

Review per-pipeline blockers in migration status. The server preserves readiness checks and rechecks authoritative invariants before moving pipelines. Movement does not complete the migration.

### 6. Complete explicitly

```shell
cluster-migrator migration complete --dry-run
cluster-migrator migration complete
```

Blocked-build finalization is a separate opt-in. Preview its candidates and blockers before authorizing it:

```shell
cluster-migrator migration complete --finalize-blocked-builds --dry-run
cluster-migrator migration complete --finalize-blocked-builds
cluster-migrator migration status
```

Both the opt-in preview and completion request require the `write_builds` API scope and the separate blocked-build finalization permission, in addition to the migration permissions above.

The preview separates each build's effective result from its server-proposed candidate outcome. `none` means no prior effective result; it does not mean the build has passed. Candidates are a bounded sample, not organization totals or authorization. `has_more` means inventory was omitted or remains unexamined, even if the sample is empty.

Finalization acceptance is not completion. Running work must finish normally. Inspect status for remaining work and failures before retrying completion.

## Command reference

| Command | Purpose |
| --- | --- |
| `migration configure --destination-cluster <cluster>` | Store the organization's fixed destination. |
| `migration status` | Show the phase, destination, available progress, and per-resource blockers. |
| `migration prepare-queues` | Explicitly return from validation to preparation. |
| `migration validate-queues` | Validate mappings before commitment. |
| `migration cutover-concurrency-groups --acknowledge-irreversible` | Commit irreversibly and authorize bulk group cutover. The acknowledgement is not needed for a dry run. |
| `migration cutover-pipelines` | Authorize bulk pipeline cutover separately from phase entry. |
| `migration complete [--finalize-blocked-builds]` | Request completion, optionally authorizing bounded blocked-build finalization. |
| `queue configure <queue>` | Map a source queue to an existing queue in the fixed destination at 0%, during preparation. |
| `queue status [queue]` | Show one queue migration or list all queue migrations. |
| `queue set-percent <queue> --to <0-100>` | Set absolute routing during preparation. |
| `queue metrics <queue>` | Compare recent source and destination activity. |
| `concurrency-group status [<scope>/<key>]` | Inspect one or all concurrency-group migrations. |

Run `cluster-migrator <command> --help` for full usage.

### Global flags

| Flag | Purpose |
| --- | --- |
| `--dry-run` | Use read-only observations to preview a mutation. Never authorize, schedule, or apply work. |
| `--json` | Write machine-readable JSON instead of terminal output. |
| `--timeout <duration>` | Maximum client command duration; defaults to `10m`. Does not cancel server-side work. |
| `--endpoint <url>` | Override the REST API endpoint; also `BUILDKITE_API_ENDPOINT`. |
| `--api-token <token>` | Supply a token; prefer `BUILDKITE_API_TOKEN` to avoid shell history and process listings. |

## Output and automation

```shell
cluster-migrator --json migration status
cluster-migrator --json migration cutover-concurrency-groups --dry-run
cluster-migrator --json queue metrics test
```

JSON stdout contains only the command's JSON response. Human guidance and progress are never mixed into it. Rejected operations and blocked, stale, or unavailable previews exit non-zero. Successful lifecycle results distinguish `accepted`, `completed`, and `no_op`. Configuration returns the stored migration directly.

Status distinguishes the configuration creator from the latest phase-transition actor, source, and time. Transition attribution comes from the recorded event; it is not inferred from configuration. Background authorization events do not change the phase version.

Status progress is a bounded sample, not an organization total. `has_more` means inventory was omitted, not that migration finished. Held group counts overlap draining and reconciling. A completed pipeline is currently in the fixed destination; this does not claim the CLI moved it. Fresh/stale describes observations only, not readiness or authorization. Unavailable observations do not imply zero work, no blockers, or absence of authorization.

Previews report observation timestamps, expiry, and cache status. They never authorize transitions. Mutations submit the current phase version; the server rejects stale versions and reruns fresh checks. The CLI does not retry authorization automatically. If a mutation times out, inspect `migration status` before retrying: server-side work may still be running.

Queue metrics may initially be unavailable while an observation is prepared. The CLI follows the retry interval for up to one minute, writes prolonged-wait progress to stderr, and keeps stdout clean for the final result. Missing metric values appear as `—`, not zero.

## Development

Install the pinned tools, build the binary, and run the checks with [mise](https://mise.jdx.dev/):

```shell
mise install
mise run build
mise run format
mise run lint
mise run test
mise run vet
mise run vulnerability
```

The build task creates `./cluster-migrator`. Run `mise run release-check` when changing release configuration. The pre-commit hook runs formatting, linting, tests, `go vet`, and release checks.

## License

MIT. See [LICENSE](LICENSE).
