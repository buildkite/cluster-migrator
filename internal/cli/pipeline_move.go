package cli

import "fmt"

type PipelineMoveCmd struct {
	Pipeline           string `arg:"" help:"Pipeline slug."`
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
		readiness, err := app.Client.GetPipelineReadiness(app.Context, cmd.Pipeline, cluster.ID)
		if err != nil {
			return fmt.Errorf("check pipeline readiness: %w", err)
		}
		if !readiness.Ready {
			return fmt.Errorf(
				"pipeline is not ready: blocking queues=%v",
				readiness.BlockingQueues,
			)
		}
		return app.Print(change)
	}
	if err := app.Confirm(change); err != nil {
		return err
	}

	pipeline, err := app.Client.MovePipeline(app.Context, cmd.Pipeline, cluster.ID)
	if err != nil {
		return fmt.Errorf("move pipeline: %w", err)
	}
	if !cmd.Wait {
		return app.Print(pipeline)
	}

	err = app.Poll(func() (bool, error) {
		pipeline, err = app.Client.GetPipeline(app.Context, cmd.Pipeline)
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
