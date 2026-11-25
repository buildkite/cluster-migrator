package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

type MigrationCmd struct {
	Configure MigrationConfigureCmd `cmd:"" help:"Configure the organization's fixed migration destination."`
	Status    MigrationStatusCmd    `cmd:"" help:"Show the destination, creation time, and current and next phases."`
	Readiness MigrationReadinessCmd `cmd:"" help:"Check one phase transition without changing phase."`
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
	return `Text status does not assess readiness. Use migration readiness to check a transition.

For compatibility, --json preserves migration fields and adds completion_readiness during migrate_pipelines. In that mode, completion blockers exit nonzero after printing the assessment.`
}

func (cmd *MigrationStatusCmd) Run(app *Context) error {
	if !app.JSON {
		migration, err := app.Client.GetMigrationSummary(app.Context)
		if err != nil {
			return fmt.Errorf("get organization migration: %w", err)
		}
		destination := migration.DestinationClusterName
		if destination == "" {
			destination = migration.DestinationClusterID
		}
		created := displayValue(migration.CreatedAt)
		if timestamp, err := time.Parse(time.RFC3339Nano, migration.CreatedAt); err == nil {
			created = timestamp.UTC().Format("2 Jan 2006 03:04 PM MST")
			days := int(app.now().Sub(timestamp).Hours() / 24)
			if days > 0 {
				created += fmt.Sprintf(" (%d %s ago)", days, pluralize(days, "day"))
			}
		}
		_, err = fmt.Fprintf(app.Output, "Destination cluster: %s\nCreated: %s\nCurrent phase: %s\nNext phase: %s\n", displayValue(destination), created, migration.Phase, displayNextPhase(migration.Phase, migration.NextPhase))
		return err
	}

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
	if err := app.Print(struct {
		*buildkite.Migration
		CompletionReadiness *buildkite.MigrationReadiness `json:"completion_readiness,omitempty"`
	}{migration, readiness}); err != nil {
		return err
	}
	if readiness != nil && !readiness.Ready {
		return fmt.Errorf("migration has known completion blockers")
	}
	return nil
}

func displayNextPhase(current, next string) string {
	if current == "complete" {
		return "None"
	}
	return displayValue(next)
}

type MigrationReadinessCmd struct {
	Phase string `arg:"" optional:"" default:"next" help:"Target phase ID (for example, validate_queues). Omit to check the next forward phase."`
}

func (cmd *MigrationReadinessCmd) Run(app *Context) error {
	readiness, err := app.Client.GetMigrationTransitionReadiness(app.Context, cmd.Phase)
	if err != nil {
		return fmt.Errorf("get migration transition readiness: %w", err)
	}
	target := readinessTarget(readiness)
	noNextPhase := cmd.Phase == "next" && readiness.Status == "no_next_phase" && readiness.Phase == "complete" && target == ""
	if !noNextPhase && (target == "" || (cmd.Phase != "next" && target != cmd.Phase) ||
		(cmd.Phase == "next" && target == "complete" && readiness.Phase != "migrate_pipelines")) {
		return fmt.Errorf("API did not assess the requested phase; check that the API supports transition readiness")
	}

	if app.JSON {
		if err := app.Print(readiness); err != nil {
			return err
		}
	} else if noNextPhase {
		_, err := fmt.Fprintln(app.Output, "The migration is complete. There is no next phase to check.")
		return err
	} else {
		if err := app.printMigrationReadiness(readiness, target); err != nil {
			return err
		}
	}
	if !noNextPhase && !readiness.Ready {
		return fmt.Errorf("migration is not ready for %s", target)
	}
	return nil
}

func readinessTarget(readiness *buildkite.MigrationReadiness) string {
	if readiness.TargetPhase == nil {
		return ""
	}
	return *readiness.TargetPhase
}

func (app *Context) printMigrationReadiness(readiness *buildkite.MigrationReadiness, target string) error {
	status := "NOT READY"
	if readiness.Ready {
		status = "READY"
	}
	if _, err := fmt.Fprintf(app.Output, "READINESS FOR %s\n\nStatus: %s\n", strings.ToUpper(strings.ReplaceAll(target, "_", " ")), status); err != nil {
		return err
	}
	if checked, err := time.Parse(time.RFC3339Nano, readiness.CheckedAt); err == nil {
		if _, err := fmt.Fprintf(app.Output, "Last checked: %s\n", checked.UTC().Format("Mon, 2 Jan 2006 15:04:05 MST")); err != nil {
			return err
		}
	}
	if readiness.Ready {
		command := "cluster-migrator migration set-phase " + strings.ReplaceAll(target, "_", "-")
		action := "Request the transition when you're ready. The server rechecks readiness before changing phase"
		switch target {
		case "prepare_queues":
			action = "Return to preparation to edit queue mappings and routing. This also admits unmapped source queues again. The server rechecks readiness before changing phase"
		case "cutover_concurrency_groups":
			action = "Request irreversible commitment only after validating workloads. The server rechecks readiness before changing phase"
			command += " --acknowledge-irreversible"
		}
		return app.printNextSteps([]string{action + ":\n\n   " + command})
	}

	var codes []string
	messages := make(map[string][]string)
	for _, blocker := range readiness.BlockerSamples {
		switch blocker.Code {
		case "already_in_phase":
			_, err := fmt.Fprintf(app.Output, "\nThe migration is already in %s.\n", target)
			return err
		case "invalid_phase":
			if _, err := fmt.Fprintf(app.Output, "\nCurrent phase: %s\nNext phase: %s\n\n", readiness.Phase, displayNextPhase(readiness.Phase, readiness.NextPhase)); err != nil {
				return err
			}
			if target == "complete" {
				_, err := fmt.Fprintln(app.Output, "Completion requires the migration to be in migrate_pipelines.\nCompletion blockers were not checked.")
				return err
			}
			_, err := fmt.Fprintln(app.Output, blocker.Message+"\nTransition blockers were not checked.")
			return err
		}
		if _, seen := messages[blocker.Code]; !seen {
			codes = append(codes, blocker.Code)
		}
		messages[blocker.Code] = append(messages[blocker.Code], blocker.Message)
	}
	for _, code := range codes {
		heading := strings.ReplaceAll(code, "_", " ")
		if heading == "" {
			heading = "blocker"
		}
		if _, err := fmt.Fprintf(app.Output, "\n%s:\n* %s\n", strings.ToUpper(heading[:1])+heading[1:], strings.Join(messages[code], "\n* ")); err != nil {
			return err
		}
		if limit := readiness.BlockerSampleLimitPerCategory; limit > 0 && len(messages[code]) == limit {
			if _, err := fmt.Fprintf(app.Output, "Showing up to %d blockers in this category.\n", limit); err != nil {
				return err
			}
		}
	}
	if target == "complete" {
		return app.printNextSteps(completionBlockerNextSteps(readiness))
	}
	if len(messages["routing_incomplete"]) > 0 || len(messages["unmapped_queue"]) > 0 {
		switch readiness.Phase {
		case "prepare_queues":
			return app.printNextSteps([]string{"Add any missing mappings and route the listed queues 100% to their destination queues. Review queue configuration with:\n\n   cluster-migrator queue status"})
		case "validate_queues":
			return app.printNextSteps([]string{"Queue configuration is frozen during validation. Check whether you can return to preparation before editing mappings or routing:\n\n   cluster-migrator migration readiness prepare_queues"})
		}
	}
	return nil
}

func completionBlockerNextSteps(readiness *buildkite.MigrationReadiness) []string {
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
	return steps
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
			"Check remaining completion blockers before requesting completion:\n\n   cluster-migrator migration readiness complete",
		}
	default:
		return nil
	}
}
