package cli

import "fmt"

type QueueConfigureCmd struct {
	Queue string `arg:"" help:"Source unclustered queue key."`
}

func (cmd *QueueConfigureCmd) Run(app *Context) error {
	migration, err := requireQueuePreparation(app)
	if err != nil {
		return err
	}
	cluster, err := app.Client.ResolveCluster(app.Context, migration.DestinationClusterID)
	if err != nil {
		return fmt.Errorf("resolve destination cluster: %w", err)
	}
	if err := app.Client.RequireClusterQueue(app.Context, cluster.ID, cmd.Queue); err != nil {
		return fmt.Errorf("validate destination queue: %w", err)
	}

	change := QueueChange{
		Command:   "CONFIGURE",
		Queue:     cmd.Queue,
		ToPercent: 0,
		Destination: QueueDestination{
			ClusterID:   cluster.ID,
			ClusterName: cluster.Name,
		},
		DryRun: app.DryRun,
	}
	if app.DryRun {
		return app.Print(change)
	}

	if _, err := app.Client.ConfigureQueueMigration(app.Context, cmd.Queue, cluster.ID); err != nil {
		return fmt.Errorf("configure queue migration: %w", err)
	}
	change.Next = queueRoutingNextSteps(cmd.Queue, 0)
	return app.Print(change)
}
