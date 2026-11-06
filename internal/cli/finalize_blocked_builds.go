package cli

import (
	"context"
	"errors"
	"fmt"
)

type FinalizeBlockedBuildsCmd struct{}

func (cmd *FinalizeBlockedBuildsCmd) Help() string {
	return `Requires migrate_pipelines, write_clusters and write_builds token scopes, and server-side organization, cluster, and build permissions. The server decides build eligibility. No dry run is available.

Acceptance starts a server-side sweep; it does not confirm that all builds are finalized. To check progress, run a readiness check for the complete phase to see remaining migration blockers. This CLI does not yet expose that check; coordinate with Buildkite to run it. There is no per-build finalization report endpoint. JSON output is {"accepted": true}, not a completion result. A client timeout does not cancel server-side finalization; check completion readiness before retrying.

Finalization does not impose a permanent reopening boundary on passed or failed builds: normal admission rules still apply, and resumed builds block completion again. Builds canceled by finalization cannot restart, including through synchronous trigger descendants.

Once all unclustered builds are terminal, request completion separately with cluster-migrator migration set-phase complete. This command does not move pipelines or change the migration phase.`
}

func (cmd *FinalizeBlockedBuildsCmd) Run(app *Context) error {
	if app.DryRun {
		return fmt.Errorf("finalize-blocked-builds does not support --dry-run: the server has no read-only readiness endpoint; no finalization was requested")
	}
	if err := app.Client.FinalizeBlockedBuilds(app.Context); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("finalize blocked builds: %w; the request may have been accepted; a client timeout does not cancel server-side finalization; coordinate with Buildkite to check readiness for the complete phase before retrying or requesting completion", err)
		}
		return fmt.Errorf("finalize blocked builds: %w", err)
	}
	if app.JSON {
		return app.Print(struct {
			Accepted bool `json:"accepted"`
		}{Accepted: true})
	}
	if _, err := fmt.Fprint(app.Output, "FINALIZE-BLOCKED-BUILDS\n\nRESULT\n\nBlocked-build finalization request accepted. Processing continues on the server; this does not confirm that all builds are finalized.\nNo pipeline moves or phase transitions were requested.\n"); err != nil {
		return err
	}
	return app.printNextSteps([]string{
		"Check readiness for the complete phase to see remaining migration blockers. This CLI does not yet expose that check; coordinate with Buildkite to run it.",
		"Once all unclustered builds are terminal, request migration completion separately:\n\n   cluster-migrator migration set-phase complete",
	})
}
