package cli

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

type QueueConfigureCmd struct {
	Queue string `arg:"" help:"Source unclustered queue key."`
}

func (cmd *QueueConfigureCmd) Run(app *Context) error {
	migration, err := requireQueuePreparation(app, "CONFIGURE", cmd.Queue)
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

func requireQueuePreparation(app *Context, command, queue string) (*buildkite.OrganizationMigration, error) {
	migration, err := app.Client.GetMigration(app.Context)
	var apiError *buildkite.APIError
	if errors.As(err, &apiError) && apiError.StatusCode == http.StatusNotFound {
		err = fmt.Errorf("organization migration is not configured; run cluster-migrator migration configure --destination-cluster <cluster>: %w", err)
	} else if err == nil && migration.Phase != "prepare_queues" {
		if migration.Phase == "validate_queues" {
			err = fmt.Errorf("queue edits require prepare_queues; explicitly return with cluster-migrator migration prepare-queues before editing")
		} else {
			err = fmt.Errorf("queue edits are not permitted in phase %q; inspect cluster-migrator migration status for forward recovery", migration.Phase)
		}
	}
	if err != nil {
		if app.DryRun && !app.JSON {
			if _, printErr := fmt.Fprintf(app.Output, "QUEUE %s (DRY RUN)\nQueue: %s\n\nREADINESS\n\nBlocked: %s.\n", command, queue, err); printErr != nil {
				return nil, printErr
			}
		}
		return nil, fmt.Errorf("check organization migration: %w", err)
	}
	return migration, nil
}
