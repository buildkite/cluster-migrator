package cli

import (
	"fmt"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

type PipelineMoveCmd struct {
	Pipeline           string `arg:"" help:"Pipeline ID, name, or slug."`
	DestinationCluster string `name:"destination-cluster" required:"" help:"Destination cluster name or ID."`
}

func (cmd *PipelineMoveCmd) Run(app *Context) error {
	cluster, err := app.Client.ResolveCluster(app.Context, cmd.DestinationCluster)
	if err != nil {
		return fmt.Errorf("resolve destination cluster: %w", err)
	}

	change := Change{
		Action:             "move",
		Resource:           "pipeline " + cmd.Pipeline,
		DestinationCluster: cluster.Name,
		DryRun:             app.DryRun,
	}
	if app.DryRun {
		readiness, err := getPipelineReadinessForPipeline(app, cmd.Pipeline, cluster.ID)
		if err != nil {
			return fmt.Errorf("check pipeline readiness: %w", err)
		}
		if readiness.Status != buildkite.PipelineReadinessNoKnownBlockers {
			if app.JSON {
				if err := app.Print(readiness); err != nil {
					return err
				}
			} else if err := app.printBlockedPipelineMoveDryRun(cmd.Pipeline, cluster.Name, readiness); err != nil {
				return err
			}
			return fmt.Errorf("pipeline has known blockers")
		}
		return app.Print(change)
	}

	pipeline, err := runWithPipelineIdentifierFallback(app.Context, app.Client, cmd.Pipeline, func(identifier string) (*buildkite.Pipeline, error) {
		return app.Client.MovePipeline(app.Context, identifier, cluster.ID)
	})
	if err != nil {
		return fmt.Errorf("move pipeline: %w", err)
	}
	return app.Print(PipelineMoveResult{Pipeline: pipeline, Destination: cluster.Name})
}

func (app *Context) printBlockedPipelineMoveDryRun(pipeline, destination string, readiness *buildkite.PipelineReadiness) error {
	if _, err := fmt.Fprintf(app.Output, "PIPELINE MOVE (DRY RUN)\nPipeline: %s\nDestination: %s\n\n", pipeline, destination); err != nil {
		return err
	}
	return app.printPipelineReadinessAssessment(readiness, "READINESS")
}
