---
name: writing-cli-commands
description: "Guides CLI command design for cluster-migrator. Use when adding commands or changing CLI output and its tests."
---

# Writing CLI Commands

Use one consistent output structure—not one presentation for every kind of data.

## Workflow

1. Read the command, its renderer, and its exact-output tests. Start with
   `internal/cli/context.go` for shared output and the nearest command-specific
   renderer for assessments or metrics. Identify the command's human output,
   JSON shape, stdout/stderr behavior, and exit behavior before changing them.
   Done when the affected contracts and applicable states are identified.
2. Draft invocation-and-output examples using the rules below. Cover success,
   no-op, dry run, blocked/error, empty, and multiple-item states where supported;
   do not add features merely to fill this list. Done when each applicable state
   has truthful wording and only justified guidance.
3. Implement with the existing rendering patterns. Keep presentation in
   `internal/cli`, not API models or client methods; do not introduce a rendering
   framework for a new command. Done when the examples are implemented without
   unrelated behavior or JSON changes.
4. Add or update exact-output tests for the affected states. Check JSON separately,
   stdout/stderr separation, exit behavior, and that dry runs make no mutation
   requests. Run `go test ./internal/cli` and broader tests if shared behavior
   changed. Inspect captured human output for alignment and readability; do not
   mutate real resources to obtain examples. Done when checks pass and affected
   README examples match the output. If verification cannot finish, report what
   remains unverified rather than treating the check as complete.

## Output grammar

- Start human-readable command results with an uppercase command heading, such
  as `QUEUE SET-PERCENT`, followed by resource identity on labeled lines, such as
  `Queue:` and `Destination:`. This is a result format, not a requirement to wrap
  parser errors, help, version output, or progress messages in report sections.
- Use this order: command heading → resource identity → outcome or assessment →
  relevant context → actionable next steps. Omit inapplicable sections.
- Use uppercase section headings. Use `RESULT` for completed mutations or explicit
  no-ops; use `STATUS`, `READINESS`, or a descriptive data heading for reads and
  assessments. Do not imply that a read performed a change.
- Separate sections with a blank line and put a blank line after each section
  heading. Use sentence case and terminal punctuation for prose; keep labels and
  table cells concise. End output with a newline.
- Report a single outcome in one plain sentence. Say what changed, including
  before/after values when useful. Say “remains” or “already” for a no-op rather
  than claiming a change.
- Use borderless, aligned tables for collections and comparisons. Prefer the
  existing `text/tabwriter` pattern. No boxed ASCII tables, side-by-side panels,
  decorative banners, or duplicate prose restating every table row.
- Explain empty results in a sentence rather than printing nothing or an empty
  table. Omit empty optional sections.

## Dry runs and truthful assessments

- Append exactly ` (DRY RUN)` to the command heading. Use `PROPOSED CHANGE`
  instead of `RESULT`, with “would…” language, never completed-action wording.
- A blocked dry run retains its dry-run heading and explains the blockers; do
  not show a proposed change that cannot proceed.
- Do not offer follow-up steps that assume the dry run actually made a change.
- Distinguish observed facts from conclusions. “No known blockers” is not proof
  of readiness. Do not turn missing data into zero, stale data into live data,
  or a latest sample into a historical maximum. Label observation windows and
  independent refresh times where they affect interpretation.

## Next steps

- Include `NEXT STEPS` only when the current state supports a useful, safe action.
  Never add an empty section, generic encouragement, or a speculative remedy.
- Always number the steps, including a single step: `1.`. Put each command on its
  own line, indented three spaces beneath its step, with a blank line before it.
- Use known resource identifiers in suggested commands. Use explicit placeholders
  such as `<percentage>` only for values the operator must choose; never invent a
  safe percentage or silently choose a consequential action.
- State prerequisites before consequential actions: “If no known blockers remain…”
  rather than unconditionally telling an operator to move a pipeline.
- Do not attach per-resource workflow guidance to an unkeyed list command.

## Automation and diagnostics

- Keep `--json` machine-readable and preserve its existing contract, including
  API-shaped responses where applicable. Never mix human headings, guidance, or
  progress into JSON stdout or add presentation-only fields to its payload.
- Write command results to stdout; diagnostics and progress to stderr. Successful
  commands should not emit unsolicited blank lines or chatter on stderr.
- Preserve meaningful non-zero exits for errors and blocked assessments. A nicely
  rendered report does not turn failure into success. Errors should identify the
  failed operation and actionable cause without exposing credentials.

## Examples

Successful mutation with one useful next step:

```text
QUEUE SET-PERCENT
Queue: default
Destination: production

RESULT

Routing changed from 10% to 25%.

NEXT STEPS

1. Review destination activity:

   cluster-migrator queue metrics default
```

Dry run without post-mutation guidance:

```text
QUEUE SET-PERCENT (DRY RUN)
Queue: default
Destination: production

PROPOSED CHANGE

Routing would change from 10% to 25%.
```

These excerpts illustrate the grammar, not exhaustive command-specific guidance.
Use the command's current contracts and tests to choose the actual next steps.
