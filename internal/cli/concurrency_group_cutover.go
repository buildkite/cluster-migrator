package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

type ConcurrencyGroupCutoverCmd struct {
	Group              string `arg:"" help:"Concurrency group as <scope>/<key>, such as custom/deploy."`
	DestinationCluster string `name:"destination-cluster" required:"" help:"Destination cluster name or ID."`
	Wait               bool   `help:"Wait for the cutover to finish."`
}

func (cmd *ConcurrencyGroupCutoverCmd) Run(app *Context) error {
	scope, key, err := parseConcurrencyGroup(cmd.Group)
	if err != nil {
		return err
	}
	cluster, err := app.Client.ResolveCluster(app.Context, cmd.DestinationCluster)
	if err != nil {
		return fmt.Errorf("resolve destination cluster: %w", err)
	}
	group, err := app.Client.GetConcurrencyGroup(app.Context, scope, key)
	if err != nil {
		return fmt.Errorf("get concurrency group: %w", err)
	}
	if len(group.BlockingQueues) > 0 {
		if !app.JSON {
			if err := cmd.printHeading(app, cluster.Name); err != nil {
				return err
			}
			count := len(group.BlockingQueues)
			if _, err := fmt.Fprintf(app.Output, "\nREADINESS\n\nBlocked by %d %s.\n", count, pluralize(count, "queue")); err != nil {
				return err
			}
			if err := app.printConcurrencyGroupBlockingQueues(group.BlockingQueues); err != nil {
				return err
			}
			if err := app.printNextSteps(concurrencyGroupBlockedNextSteps(cmd.Group, group.BlockingQueues, cluster.ID)); err != nil {
				return err
			}
		}
		return fmt.Errorf("concurrency group is blocked by queues: %s", strings.Join(group.BlockingQueues, ", "))
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

	group, err = app.Client.StartConcurrencyGroupCutover(app.Context, scope, key, cluster.ID)
	if err != nil {
		return fmt.Errorf("start concurrency-group cutover: %w", err)
	}
	if !cmd.Wait {
		return cmd.printResult(app, cluster, group)
	}

	for {
		group, err = app.Client.GetConcurrencyGroup(app.Context, scope, key)
		if err != nil {
			return fmt.Errorf("get concurrency group: %w", err)
		}
		switch group.State {
		case "clustered", "succeeded":
			return cmd.printResult(app, cluster, group)
		case "failed", "cancelled":
			return fmt.Errorf("concurrency-group cutover ended in %s", group.State)
		}
		if err := app.wait(app.Context, 2*time.Second); err != nil {
			return fmt.Errorf("wait for concurrency-group cutover: %w", err)
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

func (cmd *ConcurrencyGroupCutoverCmd) printResult(app *Context, cluster *buildkite.Cluster, group *buildkite.ConcurrencyGroup) error {
	if app.JSON {
		return app.Print(group)
	}
	if err := cmd.printHeading(app, cluster.Name); err != nil {
		return err
	}
	result := fmt.Sprintf("Cutover to the %s cluster was requested.", cluster.Name)
	next := []string{fmt.Sprintf("Check cutover progress:\n\n   cluster-migrator concurrency-group status %s", cmd.Group)}
	if cmd.Wait {
		result = fmt.Sprintf("Concurrency group moved to the %s cluster.", cluster.Name)
		next = concurrencyGroupCompletedNextSteps(cluster.ID)
	}
	if _, err := fmt.Fprintf(app.Output, "\nRESULT\n\n%s\n", result); err != nil {
		return err
	}
	if err := app.printConcurrencyGroupState(group); err != nil {
		return err
	}
	return app.printNextSteps(next)
}
