package cli

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

type QueueSetPercentCmd struct {
	Queue string `arg:"" help:"Source queue key."`
	To    int    `name:"to" required:"" help:"Absolute routed percentage."`
}

func (cmd *QueueSetPercentCmd) Run(app *Context) error {
	return setQueuePercent(app, cmd.Queue, cmd.To)
}

func setQueuePercent(app *Context, queue string, percent int) error {
	if percent < 0 || percent > 100 {
		return fmt.Errorf("--to must be between 0 and 100")
	}

	if _, err := requireQueuePreparation(app); err != nil {
		return err
	}
	current, err := app.Client.GetQueueMigration(app.Context, queue)
	if err != nil {
		var apiError *buildkite.APIError
		if errors.As(err, &apiError) && apiError.StatusCode == http.StatusNotFound {
			return fmt.Errorf("cluster queue migration for queue %q is not configured; run cluster-migrator queue configure %s while preparation is open: %w", queue, queue, err)
		}
		return fmt.Errorf("get cluster queue migration for queue %q: %w", queue, err)
	}
	cluster, err := app.Client.ResolveCluster(app.Context, current.Destination.ClusterID)
	if err != nil {
		return fmt.Errorf("resolve destination cluster: %w", err)
	}
	change := QueueChange{
		Command:     "SET-PERCENT",
		Queue:       queue,
		FromPercent: &current.RoutedPercent,
		ToPercent:   percent,
		Destination: QueueDestination{
			ClusterID:   cluster.ID,
			ClusterName: cluster.Name,
		},
		DryRun: app.DryRun,
	}
	if !app.DryRun {
		change.Next = queueRoutingNextSteps(queue, percent)
	}
	if current.RoutedPercent == percent || app.DryRun {
		return app.Print(change)
	}

	if _, err := app.Client.SetQueueMigrationPercent(app.Context, queue, percent); err != nil {
		return fmt.Errorf("set routed percentage: %w", err)
	}
	return app.Print(change)
}

func queueRoutingNextSteps(queue string, percent int) string {
	switch percent {
	case 0:
		return fmt.Sprintf("1. Scale the destination infrastructure.\n\n2. Once ready, begin routing:\n\n   cluster-migrator queue set-percent %s --to <percentage>", queue)
	case 100:
		return "1. Once all queue mappings are prepared, test representative workloads during validation:\n\n   cluster-migrator migration validate-queues\n\n2. Inspect the organization migration before further operations:\n\n   cluster-migrator migration status"
	default:
		return fmt.Sprintf("1. Review destination activity:\n\n   cluster-migrator queue metrics %s\n\n2. When ready, increase routing:\n\n   cluster-migrator queue set-percent %s --to <percentage>", queue, queue)
	}
}
