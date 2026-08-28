package cli

import "fmt"

type QueueSetPercentCmd struct {
	Queue string `arg:"" help:"Source queue key."`
	To    int    `name:"to" required:"" help:"Absolute routed percentage."`
}

func (cmd *QueueSetPercentCmd) Run(app *Context) error {
	return setQueuePercent(app, cmd.Queue, cmd.To, "")
}

func setQueuePercent(app *Context, queue string, percent int, note string) error {
	if percent < 0 || percent > 100 {
		return fmt.Errorf("--to must be between 0 and 100")
	}

	current, err := app.Client.GetQueueMigration(app.Context, queue)
	if err != nil {
		return fmt.Errorf("get queue migration: %w", err)
	}
	cluster, err := app.Client.ResolveCluster(app.Context, current.Destination.ClusterID)
	if err != nil {
		return fmt.Errorf("resolve destination cluster: %w", err)
	}
	change := QueueChange{
		Queue:       queue,
		FromPercent: &current.RoutedPercent,
		ToPercent:   percent,
		Destination: QueueDestination{
			ClusterID:   cluster.ID,
			ClusterName: cluster.Name,
		},
		DryRun: app.DryRun,
		Note:   note,
	}
	if current.RoutedPercent == percent || app.DryRun {
		return app.Print(change)
	}

	if _, err := app.Client.SetQueueMigrationPercent(app.Context, queue, percent); err != nil {
		return fmt.Errorf("set routed percentage: %w", err)
	}
	return app.Print(change)
}
