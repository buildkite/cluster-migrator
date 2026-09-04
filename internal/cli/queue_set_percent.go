package cli

import "fmt"

type QueueSetPercentCmd struct {
	Queue string `arg:"" help:"Source queue key."`
	To    int    `name:"to" required:"" help:"Absolute routed percentage."`
}

func (cmd *QueueSetPercentCmd) Run(app *Context) error {
	return setQueuePercent(app, cmd.Queue, cmd.To, "SET-PERCENT", "")
}

func setQueuePercent(app *Context, queue string, percent int, command, note string) error {
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
		Command:     command,
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
	if note == "" && percent < 100 && !app.DryRun {
		if percent == 0 {
			change.Next = fmt.Sprintf(
				"Scale the destination infrastructure. When ready, begin routing:\n\n  cluster-migrator queue set-percent %s --to <percentage>",
				queue,
			)
		} else {
			change.Next = fmt.Sprintf(
				"Review destination activity before increasing routing:\n\n  cluster-migrator queue metrics %s",
				queue,
			)
		}
	}
	if current.RoutedPercent == percent || app.DryRun {
		return app.Print(change)
	}

	if _, err := app.Client.SetQueueMigrationPercent(app.Context, queue, percent); err != nil {
		return fmt.Errorf("set routed percentage: %w", err)
	}
	return app.Print(change)
}
