package cli

import "fmt"

type QueueConfigureCmd struct {
	Queue              string `arg:"" help:"Source unclustered queue key."`
	DestinationCluster string `name:"destination-cluster" required:"" help:"Destination cluster name or ID."`
}

func (cmd *QueueConfigureCmd) Run(app *Context) error {
	cluster, err := app.Client.ResolveCluster(app.Context, cmd.DestinationCluster)
	if err != nil {
		return fmt.Errorf("resolve destination cluster: %w", err)
	}
	if err := app.Client.RequireClusterQueue(app.Context, cluster.ID, cmd.Queue); err != nil {
		return fmt.Errorf("validate destination queue: %w", err)
	}

	zero := 0
	change := Change{
		Action:             "configure",
		Resource:           "queue " + cmd.Queue,
		DestinationCluster: cluster.Name,
		ToPercent:          &zero,
		DryRun:             app.DryRun,
	}
	if app.DryRun {
		return app.Print(change)
	}

	migration, err := app.Client.ConfigureQueueMigration(app.Context, cmd.Queue, cluster.ID)
	if err != nil {
		return fmt.Errorf("configure queue migration: %w", err)
	}
	return app.Print(migration)
}
