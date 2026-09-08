<!--
Keep the PR title, description, comments, and attachments suitable for a public audience.
NEVER discuss customers or include customer names, identifying details, or customer-specific examples.
NEVER link to internal documentation or Amp threads.
Describe the problem and solution in generic, self-contained terms.
-->

## Description

<!--
- What problem are you trying to solve, and how are you solving it?
- What alternatives did you consider?
-->

## Changes

<!--
Start with a short summary of the user-visible changes.

For new or changed CLI behavior:
- Add a subsection for each affected command or behavior.
- Show a representative invocation followed by its actual stdout in a fenced
  `text` block, with `$` before the command.
- For changed output, prefer a compact before/after comparison.
- Include relevant non-default states, such as dry-run, blocked, or failed
  operations. Label stderr and non-zero exit codes separately when relevant.
- Capture output from this PR's revision using safe, generic, non-customer
  fixtures. Do not run production mutations just to obtain examples.
- Do not invent output. If execution is unavailable, state that limitation.
- Keep examples focused; clearly mark omitted output.
- Include --help output only when the help text itself is part of the change.
- Use brief prose for behavior or limitations the examples do not explain.

For changes that do not affect CLI behavior or output, use a short summary
or bullets instead.
-->

<!--
### Public documentation

Select one option in the raw PR body for the docs bot.
- [ ] Ready for public documentation - document and publish
- [ ] Not ready for public docs - document and hold
- [ ] Not applicable - docs not needed
-->

## Testing

- [ ] Tests passed locally (`mise run test`) or, for Buildkite employees, in the automatic Buildkite pipeline.
- [ ] Formatting check passed (`mise run format`).
- [ ] Lint checks passed (`mise run lint`).

<!-- Describe other verification. Note any checks that failed or could not be run locally. -->

## Deployment

<!-- Note any API dependencies, feature flags, permissions, or release ordering requirements. -->

## Roll forward

<!--
This CLI is distributed as released binaries. Reverting source does not recall installed binaries.
Describe how to recover through a corrective release and what users should do until they upgrade.
A source revert may be part of that release, but is not itself a release rollback.
Explain any side effects that require separate recovery; a new release does not undo prior CLI actions.
Keep release recovery distinct from any rollback operation provided by the CLI.
-->

## Affiliation (optional, external contributors)

<!--
If you're contributing from outside Buildkite and are comfortable sharing, name your own
company or organisation to help prioritise review. Do not disclose customer relationships
or discuss any customer's identity, use case, or circumstances.
-->
