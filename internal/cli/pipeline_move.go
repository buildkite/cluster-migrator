package cli

import (
	"fmt"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

type PipelineMoveCmd struct {
	Pipeline           string `arg:"" help:"Pipeline ID, name, or slug."`
	DestinationCluster string `name:"destination-cluster" required:"" help:"Destination cluster name or ID."`
	Wait               bool   `help:"Wait until the pipeline reports the destination cluster."`
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
			return fmt.Errorf("pipeline has known blockers")
		}
		return app.Print(change)
	}

	pipelineIdentifier := cmd.Pipeline
	pipeline, err := runWithPipelineIdentifierFallback(app.Context, app.Client, cmd.Pipeline, func(identifier string) (*buildkite.Pipeline, error) {
		pipelineIdentifier = identifier
		return app.Client.MovePipeline(app.Context, identifier, cluster.ID)
	})
	if err != nil {
		return fmt.Errorf("move pipeline: %w", err)
	}
	if !cmd.Wait {
		return app.Print(pipeline)
	}

	err = app.Poll(func() (bool, error) {
		pipeline, err = app.Client.GetPipeline(app.Context, pipelineIdentifier)
		if err != nil {
			return false, err
		}
		return pipeline.ClusterID == cluster.ID, nil
	})
	if err != nil {
		return err
	}
	return app.Print(pipeline)
}
