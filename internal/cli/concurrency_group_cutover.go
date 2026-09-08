package cli

import (
	"fmt"
	"time"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
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
		if !app.JSON {
			if err := cmd.printHeading(app, cluster.Name); err != nil {
				return err
			}
			if _, err := fmt.Fprint(app.Output, "\nREADINESS\n\nCutover is blocked by queues.\n"); err != nil {
				return err
			}
			if err := app.printConcurrencyGroupBlockingQueues(group.BlockingQueues); err != nil {
				return err
			}
		}
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
		if err := cmd.printHeading(app, cluster.Name); err != nil {
			return err
		}
		_, err := fmt.Fprintf(app.Output, "\nREADINESS\n\nNo known queue blockers. This is not proof of readiness.\n\nPROPOSED CHANGE\n\nA cutover to the %s cluster would be requested.\n", cluster.Name)
		return err
	}

	group, err = app.Client.StartConcurrencyGroupCutover(app.Context, cmd.Group, cluster.ID)
	if err != nil {
		return fmt.Errorf("start concurrency-group cutover: %w", err)
	}
	if !cmd.Wait {
		return cmd.printResult(app, cluster.Name, group)
	}

	for {
		group, err = app.Client.GetConcurrencyGroup(app.Context, cmd.Group)
		if err != nil {
			return err
		}
		switch group.State {
		case "clustered", "succeeded":
			return cmd.printResult(app, cluster.Name, group)
		case "failed", "cancelled":
			return fmt.Errorf("concurrency-group cutover ended in %s", group.State)
		}
		if err := app.wait(app.Context, 2*time.Second); err != nil {
			return err
		}
	}
}

func (cmd *ConcurrencyGroupCutoverCmd) printHeading(app *Context, destination string) error {
	heading := "CONCURRENCY-GROUP CUTOVER"
	if app.DryRun {
		heading += " (DRY RUN)"
	}
	_, err := fmt.Fprintf(app.Output, "%s\nGroup: %s\nDestination: %s\n", heading, cmd.Group, destination)
	return err
}

func (cmd *ConcurrencyGroupCutoverCmd) printResult(app *Context, destination string, group *buildkite.ConcurrencyGroup) error {
	if app.JSON {
		return app.Print(group)
	}
	if err := cmd.printHeading(app, destination); err != nil {
		return err
	}
	result := "Cutover request accepted."
	if cmd.Wait {
		result = "Cutover completed."
	}
	if _, err := fmt.Fprintf(app.Output, "\nRESULT\n\n%s\n", result); err != nil {
		return err
	}
	if err := app.printConcurrencyGroupState(group); err != nil {
		return err
	}
	if !cmd.Wait {
		_, err := fmt.Fprintf(app.Output, "\nNEXT STEPS\n\n1. Inspect cutover status:\n\n   cluster-migrator concurrency-group status %s\n", cmd.Group)
		return err
	}
	return nil
}
