package cli

import "fmt"

type PipelineReadinessCmd struct {
	Pipeline           string `arg:"" help:"Pipeline slug."`
	DestinationCluster string `name:"destination-cluster" required:"" help:"Destination cluster name or ID."`
}

func (cmd *PipelineReadinessCmd) Run(app *Context) error {
	cluster, err := app.Client.ResolveCluster(app.Context, cmd.DestinationCluster)
	if err != nil {
		return fmt.Errorf("resolve destination cluster: %w", err)
	}
	readiness, err := app.Client.GetPipelineReadiness(app.Context, cmd.Pipeline, cluster.ID)
	if err != nil {
		return fmt.Errorf("get pipeline readiness: %w", err)
	}
	if err := app.Print(readiness); err != nil {
		return err
	}
	if !readiness.Ready {
		return fmt.Errorf("pipeline is not ready to move")
	}
	return nil
}
