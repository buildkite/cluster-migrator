package cli

import (
	"fmt"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

const pipelineRollbackScope = "Queue routing percentages and concurrency-group migration state remain unchanged."
const pipelineRollbackTargets = "Cancellable states: creating, scheduled, started, failing, blocked. Already canceling builds count as pending; terminal builds (including failed) are excluded."

type PipelineRollbackCmd struct {
	Pipeline string `arg:"" help:"Pipeline ID, name, or slug."`
}

func (cmd *PipelineRollbackCmd) Run(app *Context) error {
	if app.DryRun {
		return cmd.dryRun(app)
	}
	result, err := runWithPipelineIdentifierFallback(app.Context, app.Client, cmd.Pipeline, func(identifier string) (*buildkite.PipelineRollback, error) {
		return app.Client.RollbackPipeline(app.Context, identifier)
	})
	if err != nil {
		return fmt.Errorf("rollback pipeline: %w; if the request reached the server, assignment or cancellation may already have changed; inspect pipeline and builds before retrying", err)
	}
	if app.JSON {
		err = app.Print(result)
	} else {
		err = app.printPipelineRollback(result)
	}
	if err != nil {
		return err
	}
	if result.Pending > 0 || len(result.Failures) > 0 {
		return fmt.Errorf("pipeline assignment is clear; cleanup incomplete: %d pending, %d enqueue failures", result.Pending, len(result.Failures))
	}
	return nil
}

func (cmd *PipelineRollbackCmd) dryRun(app *Context) error {
	pipeline, err := runWithPipelineIdentifierFallback(app.Context, app.Client, cmd.Pipeline, func(identifier string) (*buildkite.Pipeline, error) {
		return app.Client.GetPipeline(app.Context, identifier)
	})
	if err != nil {
		return fmt.Errorf("get pipeline for rollback dry run: %w", err)
	}
	note := "The pipeline assignment would be cleared and eligible clustered builds considered for cancellation, even if already unclustered. " +
		"The server uses a fixed cutoff of request start minus 2 hours, with two bounded best-effort passes. " +
		"Targets are not previewed; rollback permissions are not checked. No changes made. " + pipelineRollbackScope
	if app.JSON {
		return app.Print(struct {
			Action   string `json:"action"`
			Pipeline string `json:"pipeline"`
			DryRun   bool   `json:"dry_run"`
			Note     string `json:"note"`
		}{Action: "rollback", Pipeline: pipeline.Slug, DryRun: true, Note: note})
	}
	_, err = fmt.Fprintf(app.Output, "PIPELINE ROLLBACK (DRY RUN)\nPipeline: %s\n\nPROPOSED CHANGE\n\n%s\n%s\n", pipeline.Slug, note, pipelineRollbackTargets)
	return err
}

func (app *Context) printPipelineRollback(result *buildkite.PipelineRollback) error {
	assignment := "Pipeline cluster assignment cleared."
	if !result.AssignmentChanged {
		assignment = "Pipeline was already unclustered; residual cleanup attempted."
	}
	if _, err := fmt.Fprintf(app.Output, "PIPELINE ROLLBACK\nPipeline: %s\n\nRESULT\n\n%s\nCutoff (inclusive): %s\nScanned through: %s\nPasses: %d\nSelected: %d\nCancellation enqueued: %d\nStill-live clustered builds in window: %d\nEnqueue failures: %d\n",
		result.Pipeline, assignment, result.Cutoff, result.ScannedThrough, result.Passes, result.Selected, result.CancellationEnqueued, result.Pending, len(result.Failures)); err != nil {
		return err
	}
	for _, failure := range result.Failures {
		if _, err := fmt.Fprintf(app.Output, "  %s: %s\n", failure.BuildUUID, failure.Message); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(app.Output, "\nCONTEXT\n\nFixed 2-hour lookback; two best-effort passes select up to 100 builds each. Older builds and late stale creators may be missed.\n%s\nCancellation enqueued does not mean jobs have stopped or concurrency slots are released.\n%s\n",
		pipelineRollbackTargets, pipelineRollbackScope); err != nil {
		return err
	}
	if result.Pending > 0 || len(result.Failures) > 0 {
		_, err := fmt.Fprint(app.Output, "\nNEXT STEPS\n\nInspect remaining builds and enqueue failures. Rerun rollback if needed, even when already unclustered; each run uses a new 2-hour cutoff. Verify jobs and concurrency slots separately.\n")
		return err
	}
	_, err := fmt.Fprint(app.Output, "\nNo pending builds observed in this window; this is not proof of complete cleanup.\n")
	return err
}
