package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"text/tabwriter"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

type MigrationCmd struct {
	Configure                MigrationConfigureCmd                `cmd:"" help:"Configure the organization's fixed destination cluster."`
	Status                   MigrationStatusCmd                   `cmd:"" help:"Show the migration phase, progress, and blockers."`
	PrepareQueues            MigrationPrepareQueuesCmd            `cmd:"" help:"Explicitly return from validation to queue preparation."`
	ValidateQueues           MigrationValidateQueuesCmd           `cmd:"" help:"Validate queue mappings without committing cutover."`
	CutoverConcurrencyGroups MigrationCutoverConcurrencyGroupsCmd `cmd:"" help:"Irreversibly commit and authorize bulk concurrency-group cutover."`
	CutoverPipelines         MigrationCutoverPipelinesCmd         `cmd:"" help:"Authorize bulk pipeline cutover after concurrency-group completion."`
	Complete                 MigrationCompleteCmd                 `cmd:"" help:"Explicitly complete the migration after authoritative checks."`
}

type MigrationConfigureCmd struct {
	DestinationCluster string `name:"destination-cluster" required:"" help:"Fixed destination cluster name or ID."`
}

func (cmd *MigrationConfigureCmd) Run(app *Context) error {
	cluster, err := app.Client.ResolveCluster(app.Context, cmd.DestinationCluster)
	if err != nil {
		return fmt.Errorf("resolve destination cluster: %w", err)
	}
	if app.DryRun {
		return previewMigration(app, "configure", cluster.ID, false)
	}
	migration, err := app.Client.ConfigureMigration(app.Context, cluster.ID)
	if err != nil {
		return migrationMutationError("configure migration", err)
	}
	if app.JSON {
		return app.Print(migration)
	}
	if err := printMigrationHeading(app, "configure", migration); err != nil {
		return err
	}
	if _, err := fmt.Fprint(app.Output, "\nRESULT\n\nOrganization migration is configured.\n"); err != nil {
		return err
	}
	return printMigrationGuidance(app, migration.Phase)
}

type MigrationStatusCmd struct{}

func (cmd *MigrationStatusCmd) Run(app *Context) error {
	migration, err := getOrganizationMigration(app)
	if err != nil {
		return err
	}
	if app.JSON {
		return app.Print(migration)
	}
	if err := printMigrationHeading(app, "status", migration); err != nil {
		return err
	}
	if err := printMigrationAudit(app, migration); err != nil {
		return err
	}
	if err := printMigrationProgress(app, migration.Progress); err != nil {
		return err
	}
	return printMigrationGuidance(app, migration.Phase)
}

type MigrationPrepareQueuesCmd struct{}

func (cmd *MigrationPrepareQueuesCmd) Run(app *Context) error {
	return runMigrationOperation(app, "prepare-queues", buildkite.MigrationOperationRequest{})
}

type MigrationValidateQueuesCmd struct{}

func (cmd *MigrationValidateQueuesCmd) Run(app *Context) error {
	return runMigrationOperation(app, "validate-queues", buildkite.MigrationOperationRequest{})
}

type MigrationCutoverConcurrencyGroupsCmd struct {
	AcknowledgeIrreversible bool `help:"Acknowledge that commitment freezes mappings and percentages and prevents returning to preparation."`
}

func (cmd *MigrationCutoverConcurrencyGroupsCmd) Run(app *Context) error {
	if !app.DryRun && !cmd.AcknowledgeIrreversible {
		return fmt.Errorf("--acknowledge-irreversible is required: commitment freezes mappings and percentages and prevents returning to preparation; use --dry-run to preview without commitment")
	}
	return runMigrationOperation(app, "cutover-concurrency-groups", buildkite.MigrationOperationRequest{AcknowledgeIrreversible: cmd.AcknowledgeIrreversible})
}

type MigrationCutoverPipelinesCmd struct{}

func (cmd *MigrationCutoverPipelinesCmd) Run(app *Context) error {
	return runMigrationOperation(app, "cutover-pipelines", buildkite.MigrationOperationRequest{})
}

type MigrationCompleteCmd struct {
	FinalizeBlockedBuilds bool `help:"Opt in to bounded blocked-build finalization; running work must finish normally."`
}

func (cmd *MigrationCompleteCmd) Run(app *Context) error {
	return runMigrationOperation(app, "complete", buildkite.MigrationOperationRequest{FinalizeBlockedBuilds: cmd.FinalizeBlockedBuilds})
}

func getOrganizationMigration(app *Context) (*buildkite.OrganizationMigration, error) {
	migration, err := app.Client.GetMigration(app.Context)
	var apiError *buildkite.APIError
	if errors.As(err, &apiError) && apiError.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("get organization migration: %w; if not configured, run cluster-migrator migration configure --destination-cluster <cluster>", err)
	}
	if err != nil {
		return nil, fmt.Errorf("get organization migration: %w", err)
	}
	return migration, nil
}

func runMigrationOperation(app *Context, operation string, request buildkite.MigrationOperationRequest) error {
	if app.DryRun {
		return previewMigration(app, operation, "", request.FinalizeBlockedBuilds)
	}
	migration, err := getOrganizationMigration(app)
	if err != nil {
		return err
	}
	if migration.PhaseVersion == nil || *migration.PhaseVersion == "" {
		return fmt.Errorf("server did not provide phase_version; lifecycle operations are unavailable; inspect cluster-migrator migration status")
	}
	request.ExpectedPhaseVersion = *migration.PhaseVersion
	result, err := app.Client.RunMigrationOperation(app.Context, operation, request)
	if result == nil {
		return migrationMutationError(operation, err)
	}
	if app.JSON {
		if err != nil {
			var apiError *buildkite.APIError
			if errors.As(err, &apiError) {
				if printErr := app.Print(json.RawMessage(apiError.Body)); printErr != nil {
					return printErr
				}
			}
		} else if printErr := app.Print(result); printErr != nil {
			return printErr
		}
	} else {
		if result.Migration != nil {
			migration = result.Migration
		}
		if printErr := printMigrationHeading(app, operation, migration); printErr != nil {
			return printErr
		}
		message := map[string]string{
			"accepted":  "Accepted. Background work is not complete.",
			"completed": "Operation completed.",
			"no_op":     "No change was needed.",
			"blocked":   "Operation blocked without authorization.",
		}[result.Outcome]
		if message == "" {
			return fmt.Errorf("unknown migration operation outcome %q; inspect cluster-migrator migration status before retrying", result.Outcome)
		}
		if _, printErr := fmt.Fprintf(app.Output, "\nRESULT\n\n%s\n", message); printErr != nil {
			return printErr
		}
		if printErr := printMigrationAssessment(app, result.Readiness); printErr != nil {
			return printErr
		}
		if printErr := app.printNextSteps([]string{"Inspect server-side progress and blockers; a client timeout does not cancel server-side work:\n\n   cluster-migrator migration status"}); printErr != nil {
			return printErr
		}
	}
	if err != nil {
		return migrationMutationError(operation, err)
	}
	switch result.Outcome {
	case "accepted", "completed", "no_op":
		return nil
	default:
		return fmt.Errorf("unexpected migration operation outcome %q; inspect cluster-migrator migration status before retrying", result.Outcome)
	}
}

func previewMigration(app *Context, operation, destinationClusterID string, finalizeBlockedBuilds bool) error {
	assessment, err := app.Client.GetMigrationReadiness(app.Context, operation, destinationClusterID, finalizeBlockedBuilds)
	if err != nil {
		return fmt.Errorf("preview migration %s: %w", operation, err)
	}
	if app.JSON {
		if err := app.Print(assessment); err != nil {
			return err
		}
	} else {
		if err := printMigrationHeading(app, operation, nil); err != nil {
			return err
		}
		if destinationClusterID != "" {
			if _, err := fmt.Fprintf(app.Output, "Destination: %s\n", destinationClusterID); err != nil {
				return err
			}
		}
		if err := printMigrationAssessment(app, assessment); err != nil {
			return err
		}
		if assessment.Ready && assessment.Status == "ready" {
			if _, err := fmt.Fprintf(app.Output, "\nPROPOSED CHANGE\n\nThe %s operation would be requested. This preview does not authorize a transition or schedule work.\n", operation); err != nil {
				return err
			}
		}
	}
	if !assessment.Ready || assessment.Status != "ready" {
		return fmt.Errorf("migration %s preview is %s; no authorization was recorded", operation, assessment.Status)
	}
	return nil
}

func printMigrationHeading(app *Context, operation string, migration *buildkite.OrganizationMigration) error {
	heading := "MIGRATION " + strings.ToUpper(operation)
	if app.DryRun {
		heading += " (DRY RUN)"
	}
	if _, err := fmt.Fprintln(app.Output, heading); err != nil {
		return err
	}
	if migration != nil {
		_, err := fmt.Fprintf(app.Output, "Destination: %s\nPhase: %s\n", migration.DestinationClusterID, migration.Phase)
		return err
	}
	return nil
}

func printMigrationAssessment(app *Context, assessment *buildkite.MigrationAssessment) error {
	if assessment == nil {
		return nil
	}
	if _, err := fmt.Fprintf(app.Output, "\nREADINESS\n\nStatus: %s\nAssessed phase: %s\nObserved at: %s\nExpires at: %s\nCached: %t\n", assessment.Status, optionalString(assessment.Phase), displayValue(assessment.ObservedAt), optionalString(assessment.ExpiresAt), assessment.Cached); err != nil {
		return err
	}
	if transition := assessment.Transition; transition != nil {
		if _, err := fmt.Fprintf(app.Output, "Requested transition: %s → %s\n", transition.From, transition.To); err != nil {
			return err
		}
	}
	if len(assessment.Blockers) == 0 {
		return nil
	}
	if _, err := fmt.Fprint(app.Output, "\nBLOCKERS\n\n"); err != nil {
		return err
	}
	writer := tabwriter.NewWriter(app.Output, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "CODE\tBLOCKER")
	for _, blocker := range assessment.Blockers {
		_, _ = fmt.Fprintf(writer, "%s\t%s\n", blocker.Code, blocker.Message)
	}
	return writer.Flush()
}

func optionalString(value *string) string {
	if value == nil {
		return "—"
	}
	return displayValue(*value)
}

func printMigrationGuidance(app *Context, phase string) error {
	var guidance []string
	switch phase {
	case "prepare_queues":
		guidance = []string{"Map source queues to existing destination queues and adjust routing, then validate:\n\n   cluster-migrator migration validate-queues"}
	case "validate_queues":
		guidance = []string{
			"To repair mappings or percentages, explicitly return to preparation:\n\n   cluster-migrator migration prepare-queues",
			"After validation and readiness checks, preview irreversible bulk commitment:\n\n   cluster-migrator migration cutover-concurrency-groups --dry-run",
		}
	case "cutover_concurrency_groups":
		guidance = []string{"Mappings and percentages are frozen. Concurrency-group completion automatically advances to migrate_pipelines; pipeline cutover still requires explicit authorization:\n\n   cluster-migrator migration status"}
	case "migrate_pipelines":
		guidance = []string{"Review pipeline blockers before explicitly authorizing bulk pipeline cutover:\n\n   cluster-migrator migration cutover-pipelines --dry-run"}
	case "complete":
		return nil
	default:
		return nil
	}
	return app.printNextSteps(guidance)
}

func migrationMutationError(operation string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("%s request ended; server-side work may continue; inspect cluster-migrator migration status before retrying: %w", operation, err)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
