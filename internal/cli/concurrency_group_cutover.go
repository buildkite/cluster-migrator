package cli

import "fmt"

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
		return app.Print(change)
	}
	if err := app.Confirm(change); err != nil {
		return err
	}

	group, err = app.Client.StartConcurrencyGroupCutover(app.Context, cmd.Group, cluster.ID)
	if err != nil {
		return fmt.Errorf("start concurrency-group cutover: %w", err)
	}
	if !cmd.Wait {
		return app.Print(group)
	}

	err = app.Poll(func() (bool, error) {
		group, err = app.Client.GetConcurrencyGroup(app.Context, cmd.Group)
		if err != nil {
			return false, err
		}
		switch group.State {
		case "clustered", "succeeded":
			return true, nil
		case "failed", "cancelled":
			return false, fmt.Errorf("concurrency-group cutover ended in %s", group.State)
		default:
			return false, nil
		}
	})
	if err != nil {
		return err
	}
	return app.Print(group)
}
