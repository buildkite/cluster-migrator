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
	if note == "" && !app.DryRun {
		change.Next = queueRoutingNextSteps(queue, cluster.Name, percent)
	}
	if current.RoutedPercent == percent || app.DryRun {
		return app.Print(change)
	}

	if _, err := app.Client.SetQueueMigrationPercent(app.Context, queue, percent); err != nil {
		return fmt.Errorf("set routed percentage: %w", err)
	}
	return app.Print(change)
}

func queueRoutingNextSteps(queue, destination string, percent int) string {
	switch percent {
	case 0:
		return fmt.Sprintf("1. Scale the destination infrastructure.\n\n2. Once ready, begin routing:\n\n   cluster-migrator queue set-percent %s --to <percentage>", queue)
	case 100:
		return fmt.Sprintf("1. Assess each pipeline using this queue:\n\n   cluster-migrator pipeline readiness <pipeline> --destination-cluster %s\n\n2. If no known blockers remain, move the pipeline:\n\n   cluster-migrator pipeline move <pipeline> --destination-cluster %s", destination, destination)
	default:
		return fmt.Sprintf("1. Review destination activity:\n\n   cluster-migrator queue metrics %s\n\n2. When ready, increase routing:\n\n   cluster-migrator queue set-percent %s --to <percentage>", queue, queue)
	}
}
