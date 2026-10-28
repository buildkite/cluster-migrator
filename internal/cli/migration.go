package cli

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

type MigrationCmd struct {
	Configure             MigrationConfigureCmd             `cmd:"" help:"Configure the organization's fixed migration destination."`
	Status                MigrationStatusCmd                `cmd:"" help:"Show the organization's configured migration and phase."`
	SetPhase              MigrationSetPhaseCmd              `cmd:"" help:"Request a migration phase transition, subject to server readiness checks."`
	FinalizeBlockedBuilds MigrationFinalizeBlockedBuildsCmd `cmd:"" help:"Request blocked-build finalization without changing phase (requires a companion server endpoint)."`
}

type MigrationSetPhaseCmd struct {
	ValidateQueues           MigrationValidateQueuesCmd           `cmd:"" help:"Reject unmapped source queues and freeze queue configuration."`
	PrepareQueues            MigrationPrepareQueuesCmd            `cmd:"" help:"Return from validation to editable queue preparation."`
	CutoverConcurrencyGroups MigrationCutoverConcurrencyGroupsCmd `cmd:"" help:"Request irreversible concurrency-group commitment."`
	MigratePipelines         MigrationMigratePipelinesCmd         `cmd:"" help:"Enter the pipeline-migration phase without moving pipelines."`
	Complete                 MigrationCompleteCmd                 `cmd:"" help:"Request completion without finalizing blocked builds."`
}

type MigrationConfigureCmd struct {
	DestinationCluster string `name:"destination-cluster" required:"" help:"Destination cluster name or ID."`
}

func (cmd *MigrationConfigureCmd) Run(app *Context) error {
	if app.DryRun {
		return fmt.Errorf("migration configure does not support --dry-run: the server has no read-only readiness endpoint; no migration was configured")
	}
	cluster, err := app.Client.ResolveCluster(app.Context, cmd.DestinationCluster)
	if err != nil {
		return fmt.Errorf("resolve destination cluster: %w", err)
	}
	migration, err := app.Client.ConfigureMigration(app.Context, cluster.ID)
	if err != nil {
		var apiError *buildkite.APIError
		if errors.As(err, &apiError) && apiError.StatusCode == http.StatusConflict {
			return fmt.Errorf("configure organization migration: %w; use cluster-migrator migration status to inspect it", err)
		}
		return fmt.Errorf("configure organization migration: %w", err)
	}
	if app.JSON {
		return app.Print(migration)
	}
	if _, err := fmt.Fprintf(app.Output, "MIGRATION CONFIGURE\nMigration: %s\nDestination: %s\n\nRESULT\n\nMigration configured in queue preparation. No queue mappings or routing percentages were changed.\n", migration.ID, migration.DestinationClusterID); err != nil {
		return err
	}
	return app.printNextSteps(migrationNextSteps(migration.Phase))
}

type MigrationStatusCmd struct{}

func (cmd *MigrationStatusCmd) Run(app *Context) error {
	migration, err := app.Client.GetMigration(app.Context)
	if err != nil {
		return fmt.Errorf("get organization migration: %w", err)
	}
	if app.JSON {
		return app.Print(migration)
	}
	if _, err := fmt.Fprintf(app.Output, "MIGRATION STATUS\nMigration: %s\nDestination: %s\n\nSTATUS\n\nPhase: %s\n", migration.ID, migration.DestinationClusterID, migration.Phase); err != nil {
		return err
	}
	return app.printNextSteps(migrationNextSteps(migration.Phase))
}

type MigrationValidateQueuesCmd struct{}

func (cmd *MigrationValidateQueuesCmd) Run(app *Context) error {
	return changeQueuePhase(app, "validate-queues")
}

type MigrationPrepareQueuesCmd struct{}

func (cmd *MigrationPrepareQueuesCmd) Run(app *Context) error {
	return changeQueuePhase(app, "prepare-queues")
}

type MigrationCutoverConcurrencyGroupsCmd struct {
	AcknowledgeIrreversible bool `help:"Acknowledge that commitment freezes queue mappings and routing percentages and prevents reopening preparation."`
}

func (cmd *MigrationCutoverConcurrencyGroupsCmd) Run(app *Context) error {
	return runMigrationTransition(app, "set-phase cutover-concurrency-groups", func() (*buildkite.Migration, error) {
		if !cmd.AcknowledgeIrreversible {
			return nil, fmt.Errorf("requires --acknowledge-irreversible: commitment freezes queue mappings and routing percentages and prevents reopening preparation")
		}
		return app.Client.CutoverConcurrencyGroups(app.Context)
	})
}

type MigrationMigratePipelinesCmd struct{}

func (cmd *MigrationMigratePipelinesCmd) Run(app *Context) error {
	return runMigrationTransition(app, "set-phase migrate-pipelines", func() (*buildkite.Migration, error) {
		return app.Client.CutoverPipelines(app.Context)
	})
}

type MigrationCompleteCmd struct{}

func (cmd *MigrationCompleteCmd) Run(app *Context) error {
	return runMigrationTransition(app, "set-phase complete", func() (*buildkite.Migration, error) {
		return app.Client.CompleteMigration(app.Context)
	})
}

type MigrationFinalizeBlockedBuildsCmd struct{}

func (cmd *MigrationFinalizeBlockedBuildsCmd) Run(app *Context) error {
	if app.DryRun {
		return fmt.Errorf("migration finalize-blocked-builds does not support --dry-run: the server has no read-only readiness endpoint; use cluster-migrator migration status to inspect the current phase")
	}
	migration, err := app.Client.FinalizeBlockedBuilds(app.Context)
	if err != nil {
		return fmt.Errorf("migration finalize-blocked-builds: %w", err)
	}
	if app.JSON {
		return app.Print(migration)
	}
	_, err = fmt.Fprintf(app.Output, "MIGRATION FINALIZE-BLOCKED-BUILDS\nMigration: %s\nDestination: %s\n\nRESULT\n\nBlocked-build finalization requested. This does not complete the migration.\nPhase: %s\n", migration.ID, migration.DestinationClusterID, migration.Phase)
	return err
}

func runMigrationTransition(app *Context, operation string, transition func() (*buildkite.Migration, error)) error {
	if app.DryRun {
		return fmt.Errorf("migration %s does not support --dry-run: the server has no read-only readiness endpoint; use cluster-migrator migration status to inspect the current phase", operation)
	}
	migration, err := transition()
	if err != nil {
		return fmt.Errorf("migration %s: %w", operation, err)
	}
	if app.JSON {
		return app.Print(migration)
	}
	if _, err := fmt.Fprintf(app.Output, "MIGRATION %s\nMigration: %s\nDestination: %s\n\nRESULT\n\nPhase: %s\n", strings.ToUpper(operation), migration.ID, migration.DestinationClusterID, migration.Phase); err != nil {
		return err
	}
	return app.printNextSteps([]string{"Inspect the migration's current state:\n\n   cluster-migrator migration status"})
}

func changeQueuePhase(app *Context, operation string) error {
	// There is no read-only readiness endpoint for these operations.
	if app.DryRun {
		return fmt.Errorf("migration set-phase %s does not support --dry-run: the server has no read-only readiness endpoint; use cluster-migrator migration status to inspect the current phase", operation)
	}

	var migration *buildkite.Migration
	var err error
	if operation == "prepare-queues" {
		migration, err = app.Client.PrepareQueues(app.Context)
	} else {
		migration, err = app.Client.ValidateQueues(app.Context)
	}
	if err != nil {
		return fmt.Errorf("migration set-phase %s: %w", operation, err)
	}
	if app.JSON {
		return app.Print(migration)
	}
	result := "Queue validation started. Unmapped source queues are rejected; queue mappings and routing percentages are frozen."
	if operation == "prepare-queues" {
		result = "Queue preparation reopened. Queue mappings and routing percentages can be changed; unmapped source queues are admitted again."
	}
	if _, err := fmt.Fprintf(app.Output, "MIGRATION SET-PHASE %s\nMigration: %s\nDestination: %s\n\nRESULT\n\n%s\nConcurrency authority is unchanged.\n", strings.ToUpper(operation), migration.ID, migration.DestinationClusterID, result); err != nil {
		return err
	}
	return app.printNextSteps(migrationNextSteps(migration.Phase))
}

func migrationNextSteps(phase string) []string {
	switch phase {
	case "prepare_queues":
		return []string{
			"Review queue mappings:\n\n   cluster-migrator queue status",
			"Configure missing mappings and adjust routing while preparation is open.",
			"Once the mappings are ready, start validation:\n\n   cluster-migrator migration set-phase validate-queues",
		}
	case "validate_queues":
		return []string{
			"Run representative workloads to check for unmapped source queues.",
			"If you need to repair mappings or change routing, return to preparation first:\n\n   cluster-migrator migration set-phase prepare-queues",
			"After making those changes, start validation again:\n\n   cluster-migrator migration set-phase validate-queues",
		}
	case "cutover_concurrency_groups":
		return []string{
			"Inspect concurrency-group migration progress:\n\n   cluster-migrator concurrency-group status",
			"If progress stalls or an operation fails, contact Buildkite. Queue configuration is frozen and preparation cannot be reopened.",
		}
	case "migrate_pipelines":
		return []string{
			"Coordinate pipeline cutover and completion with Buildkite. The server readiness checks for these transitions are not implemented yet.",
		}
	default:
		return nil
	}
}
