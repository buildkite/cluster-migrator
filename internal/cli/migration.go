package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

type MigrationCmd struct {
	Configure MigrationConfigureCmd `cmd:"" help:"Configure the organization's fixed migration destination."`
	Status    MigrationStatusCmd    `cmd:"" help:"Show migration phase and completion readiness during pipeline migration."`
	SetPhase  MigrationSetPhaseCmd  `cmd:"" help:"Request a migration phase transition, subject to server readiness checks."`
}

type MigrationSetPhaseCmd struct {
	ValidateQueues           MigrationValidateQueuesCmd           `cmd:"" help:"Reject unmapped source queues and freeze queue configuration."`
	PrepareQueues            MigrationPrepareQueuesCmd            `cmd:"" help:"Return from validation to editable queue preparation."`
	CutoverConcurrencyGroups MigrationCutoverConcurrencyGroupsCmd `cmd:"" help:"Request irreversible commitment without starting group cutovers."`
	MigratePipelines         MigrationMigratePipelinesCmd         `cmd:"" help:"Enter the pipeline-migration phase without moving pipelines or finalizing builds."`
	Complete                 MigrationCompleteCmd                 `cmd:"" help:"Request completion without finalizing blocked builds."`
}

type MigrationConfigureCmd struct {
	DestinationCluster string `name:"destination-cluster" required:"" help:"Destination cluster name or ID."`
}

func (cmd *MigrationConfigureCmd) Run(app *Context) error {
	cluster, err := app.Client.ResolveCluster(app.Context, cmd.DestinationCluster)
	if err != nil {
		return fmt.Errorf("resolve destination cluster: %w", err)
	}
	app.LastKnown = []string{"Destination: " + cluster.ID}
	migration, err := app.Client.ConfigureMigration(app.Context, cluster.ID)
	if err != nil {
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

func (cmd *MigrationStatusCmd) Help() string {
	return `During migrate_pipelines, fetches read-only completion readiness and up to the server's blocker sample limit per category. Samples are not totals or progress percentages. Other phases show the persisted migration without requesting completion readiness.

Blocked completion readiness exits non-zero after printing the assessment. Ready means completion can be requested, not that migration is complete; completion rechecks readiness. No work is started by status.

JSON preserves migration fields and adds completion_readiness during migrate_pipelines, containing the readiness API response without human guidance. Requires the read-only /migration/readiness endpoint to be deployed. Individual pipeline readiness remains available.`
}

func (cmd *MigrationStatusCmd) Run(app *Context) error {
	migration, err := app.Client.GetMigration(app.Context)
	if err != nil {
		return fmt.Errorf("get organization migration: %w", err)
	}
	app.rememberMigration(migration)
	var readiness *buildkite.MigrationReadiness
	if migration.Phase == "migrate_pipelines" {
		readiness, err = app.Client.GetMigrationReadiness(app.Context)
		if err != nil {
			return fmt.Errorf("get migration completion readiness: %w", err)
		}
	}
	if app.JSON {
		if err := app.Print(struct {
			*buildkite.Migration
			CompletionReadiness *buildkite.MigrationReadiness `json:"completion_readiness,omitempty"`
		}{migration, readiness}); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(app.Output, "MIGRATION STATUS\nMigration: %s\nDestination: %s\n\nSTATUS\n\nPhase: %s\n", migration.ID, migration.DestinationClusterID, migration.Phase); err != nil {
			return err
		}
		if readiness != nil {
			if err := app.printMigrationReadiness(readiness); err != nil {
				return err
			}
		} else if err := app.printNextSteps(migrationNextSteps(migration.Phase)); err != nil {
			return err
		}
	}
	if readiness != nil && !readiness.Ready {
		return fmt.Errorf("migration has known completion blockers")
	}
	return nil
}

func (app *Context) printMigrationReadiness(readiness *buildkite.MigrationReadiness) error {
	if _, err := fmt.Fprintf(app.Output, "\nCOMPLETION READINESS\n\nTarget phase: %s\n", readiness.TargetPhase); err != nil {
		return err
	}
	if readiness.Ready {
		if _, err := fmt.Fprintln(app.Output, "No known completion blockers. This does not complete the migration."); err != nil {
			return err
		}
		return app.printNextSteps([]string{"Request completion explicitly. The server rechecks readiness when applying it:\n\n   cluster-migrator migration set-phase complete"})
	}
	if _, err := fmt.Fprintln(app.Output, "Completion is blocked."); err != nil {
		return err
	}
	if len(readiness.BlockerSamples) > 0 {
		if _, err := fmt.Fprintf(app.Output, "\nBLOCKER SAMPLES\n\nUp to %d per category; these are samples, not totals.\n\n", readiness.BlockerSampleLimitPerCategory); err != nil {
			return err
		}
		writer := tabwriter.NewWriter(app.Output, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(writer, "CODE\tBLOCKER")
		for _, blocker := range readiness.BlockerSamples {
			_, _ = fmt.Fprintf(writer, "%s\t%s\n", displayValue(blocker.Code), displayValue(blocker.Message))
		}
		if err := writer.Flush(); err != nil {
			return err
		}
	}
	return app.printNextSteps(migrationReadinessNextSteps(readiness))
}

func migrationReadinessNextSteps(readiness *buildkite.MigrationReadiness) []string {
	if readiness.Phase != "migrate_pipelines" {
		return []string{"The migration phase changed. Inspect its current state before further operations:\n\n   cluster-migrator migration status"}
	}
	var steps []string
	seen := make(map[string]bool)
	for _, blocker := range readiness.BlockerSamples {
		step := "Contact Buildkite to resolve the reported blocker. Queue configuration is frozen; preparation cannot be reopened."
		switch blocker.Code {
		case "unclustered_pipeline":
			step = "Assess and move remaining unclustered pipelines individually, including archived pipelines. Start with:\n\n   cluster-migrator pipeline readiness <pipeline>"
		case "unfinished_unclustered_build":
			step = "Let running work finish normally. If blocked builds need finalization, request it separately:\n\n   cluster-migrator finalize-blocked-builds"
		case "concurrency_group_migration_incomplete":
			if blocker.Scope != nil && blocker.Key != nil {
				step = fmt.Sprintf("Inspect the concurrency group's progress:\n\n   cluster-migrator concurrency-group status %s/%s", *blocker.Scope, *blocker.Key)
			}
		}
		if !seen[step] {
			steps = append(steps, step)
			seen[step] = true
		}
	}
	return append(steps, "Check completion readiness again after resolving blockers:\n\n   cluster-migrator migration status")
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

func runMigrationTransition(app *Context, operation string, transition func() (*buildkite.Migration, error)) error {
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
			"Once validation is satisfactory, all queues route 100%, and Buildkite confirms the safety gates, explicitly request irreversible commitment:\n\n   cluster-migrator migration set-phase cutover-concurrency-groups --acknowledge-irreversible",
		}
	case "cutover_concurrency_groups":
		return []string{
			"Coordinate with Buildkite to identify required groups, then request each cutover separately:\n\n   cluster-migrator concurrency-group cutover <scope>/<key>",
			"Inspect concurrency-group migration progress. This lists migration records, not every group that needs cutover:\n\n   cluster-migrator concurrency-group status",
			"If progress stalls or an operation fails, contact Buildkite. Queue configuration is frozen and preparation cannot be reopened.",
			"Once required group work has drained or moved and cutovers have finished and released their holds, request pipeline-phase entry. This does not move pipelines or finalize builds:\n\n   cluster-migrator migration set-phase migrate-pipelines",
		}
	case "migrate_pipelines":
		return []string{
			"Phase entry does not move pipelines. Assess each pipeline individually:\n\n   cluster-migrator pipeline readiness <pipeline>",
			"If no known blockers remain and operational checks are satisfactory, move the pipeline. Repeat for every unclustered pipeline, including archived pipelines:\n\n   cluster-migrator pipeline move <pipeline>",
			"If blocked builds need finalization, request it separately. Acceptance does not confirm completion:\n\n   cluster-migrator finalize-blocked-builds",
			"Check remaining completion blockers before requesting completion:\n\n   cluster-migrator migration status",
		}
	default:
		return nil
	}
}
