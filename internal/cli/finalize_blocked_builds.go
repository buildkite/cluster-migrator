package cli

import "fmt"

type FinalizeBlockedBuildsCmd struct{}

func (cmd *FinalizeBlockedBuildsCmd) Help() string {
	return `Requires migrate_pipelines, write_clusters and write_builds token scopes, and server-side organization, cluster, and build permissions. The server decides build eligibility.

Acceptance starts a server-side sweep; it does not confirm that all builds are finalized. To check remaining completion blockers, run cluster-migrator migration status. This reports blocker samples, not finalization totals. There is no per-build finalization report endpoint. JSON output is {"accepted": true}, not a completion result. A client timeout does not cancel server-side finalization; check completion readiness before retrying.

Finalization does not impose a permanent reopening boundary on passed or failed builds: normal admission rules still apply, and resumed builds block completion again. Builds canceled by finalization cannot restart, including through synchronous trigger descendants.

Once no known completion blockers remain, request completion separately with cluster-migrator migration set-phase complete. The server rechecks readiness. This command does not move pipelines or change the migration phase.`
}

func (cmd *FinalizeBlockedBuildsCmd) Run(app *Context) error {
	if err := app.Client.FinalizeBlockedBuilds(app.Context); err != nil {
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
		"Check remaining completion blockers:\n\n   cluster-migrator migration status",
		"Once no known completion blockers remain, request migration completion separately. The server rechecks readiness:\n\n   cluster-migrator migration set-phase complete",
	})
}
