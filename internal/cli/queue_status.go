package cli

import (
	"fmt"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

type QueueStatusCmd struct {
	Queue string `arg:"" optional:"" help:"Source queue key. Omit to list every migration."`
}

func (cmd *QueueStatusCmd) Run(app *Context) error {
	clusters, err := app.Client.ListClusters(app.Context)
	if err != nil {
		return fmt.Errorf("list clusters: %w", err)
	}
	clusterNames := make(map[string]string, len(clusters))
	for _, cluster := range clusters {
		clusterNames[cluster.ID] = cluster.Name
	}

	if cmd.Queue == "" {
		migrations, err := app.Client.ListQueueMigrations(app.Context)
		if err != nil {
			return fmt.Errorf("list queue migrations: %w", err)
		}
		statuses := make([]QueueStatus, 0, len(migrations))
		for i := range migrations {
			statuses = append(statuses, queueStatus(&migrations[i], clusterNames))
		}
		return app.Print(statuses)
	}

	migration, err := app.Client.GetQueueMigration(app.Context, cmd.Queue)
	if err != nil {
		return fmt.Errorf("get queue migration: %w", err)
	}
	return app.Print(queueStatus(migration, clusterNames))
}

func queueStatus(migration *buildkite.QueueMigration, clusterNames map[string]string) QueueStatus {
	destinationName := clusterNames[migration.Destination.ClusterID]
	if destinationName == "" {
		destinationName = migration.Destination.ClusterID
	}
	status := QueueStatus{
		Queue:          migration.QueueKey,
		RoutingPercent: migration.RoutedPercent,
		Destination: QueueDestination{
			ClusterID:   migration.Destination.ClusterID,
			ClusterName: destinationName,
		},
	}
	if migration.RoutedPercent == 0 {
		status.Next = fmt.Sprintf(
			"1. Scale the destination infrastructure.\n2. Once applied, begin routing:\n\n   cluster-migrator queue set-percent %s --to <percentage>",
			migration.QueueKey,
		)
	}
	return status
}
