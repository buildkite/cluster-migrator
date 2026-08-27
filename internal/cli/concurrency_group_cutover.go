package cli

import (
	"fmt"
	"time"
)

type ConcurrencyGroupCutoverCmd struct {
	Group              string `arg:"" help:"Concurrency group key."`
	DestinationCluster string `name:"destination-cluster" required:"" help:"Destination cluster name or ID."`
	Wait               bool   `help:"Wait for the cutover to finish."`
}

func (cmd *ConcurrencyGroupCutoverCmd) Run(app *Context) error {
	cluster, err := app.Client.ResolveCluster(app.Context, cmd.DestinationCluster)
	if err != nil {
		return fmt.Errorf("resolve destination cluster: %w", err)
	}
	group, err := app.Client.GetConcurrencyGroup(app.Context, cmd.Group)
	if err != nil {
		return fmt.Errorf("get concurrency group: %w", err)
	}
	if len(group.BlockingQueues) > 0 {
		return fmt.Errorf("concurrency group is blocked by queues: %v", group.BlockingQueues)
	}

	change := Change{
		Action:             "cut over",
		Resource:           "concurrency group " + cmd.Group,
		DestinationCluster: cluster.Name,
		DryRun:             app.DryRun,
	}
	if app.DryRun {
		if app.JSON {
			return app.Print(change)
		}
		_, err := fmt.Fprintf(app.Output, "%s %s in %s (dry run)\n", change.Action, change.Resource, change.DestinationCluster)
		return err
	}

	group, err = app.Client.StartConcurrencyGroupCutover(app.Context, cmd.Group, cluster.ID)
	if err != nil {
		return fmt.Errorf("start concurrency-group cutover: %w", err)
	}
	if !cmd.Wait {
		return app.Print(group)
	}

	for {
		group, err = app.Client.GetConcurrencyGroup(app.Context, cmd.Group)
		if err != nil {
			return err
		}
		switch group.State {
		case "clustered", "succeeded":
			return app.Print(group)
		case "failed", "cancelled":
			return fmt.Errorf("concurrency-group cutover ended in %s", group.State)
		}
		if err := app.wait(app.Context, 2*time.Second); err != nil {
			return err
		}
	}
}
