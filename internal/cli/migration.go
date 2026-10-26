package cli

import (
	"fmt"
	"strings"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

type MigrationCmd struct {
	Status         MigrationStatusCmd         `cmd:"" help:"Show the organization's configured migration and phase."`
	ValidateQueues MigrationValidateQueuesCmd `cmd:"" help:"Reject unmapped source queues and freeze queue configuration."`
	PrepareQueues  MigrationPrepareQueuesCmd  `cmd:"" help:"Return from validation to editable queue preparation."`
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

func changeQueuePhase(app *Context, operation string) error {
	// There is no read-only readiness endpoint for these operations.
	if app.DryRun {
		return fmt.Errorf("migration %s does not support --dry-run: the server has no read-only readiness endpoint; use cluster-migrator migration status to inspect the current phase", operation)
	}

	var migration *buildkite.Migration
	var err error
	if operation == "prepare-queues" {
		migration, err = app.Client.PrepareQueues(app.Context)
	} else {
		migration, err = app.Client.ValidateQueues(app.Context)
	}
	if err != nil {
		return fmt.Errorf("migration %s: %w", operation, err)
	}
	if app.JSON {
		return app.Print(migration)
	}
	result := "Queue validation started. Unmapped source queues are rejected; queue mappings and routing percentages are frozen."
	if operation == "prepare-queues" {
		result = "Queue preparation reopened. Queue mappings and routing percentages can be changed; unmapped source queues are admitted again."
	}
	if _, err := fmt.Fprintf(app.Output, "MIGRATION %s\nMigration: %s\nDestination: %s\n\nRESULT\n\n%s\nConcurrency authority is unchanged.\n", strings.ToUpper(operation), migration.ID, migration.DestinationClusterID, result); err != nil {
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
			"Once the mappings are ready, start validation:\n\n   cluster-migrator migration validate-queues",
		}
	case "validate_queues":
		return []string{
			"Run representative workloads to check for unmapped source queues.",
			"If you need to repair mappings or change routing, return to preparation first:\n\n   cluster-migrator migration prepare-queues",
			"After making those changes, start validation again:\n\n   cluster-migrator migration validate-queues",
		}
	case "cutover_concurrency_groups":
		return []string{
			"Inspect concurrency-group migration progress:\n\n   cluster-migrator concurrency-group status",
			"If progress stalls or an operation fails, contact Buildkite. Queue configuration is frozen and preparation cannot be reopened.",
		}
	case "migrate_pipelines":
		return []string{
			"Coordinate the remaining pipeline moves with Buildkite. This CLI does not yet provide organization-wide pipeline cutover or migration completion.",
		}
	default:
		return nil
	}
}
