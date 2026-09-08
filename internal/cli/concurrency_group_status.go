package cli

import (
	"fmt"
	"strings"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

type ConcurrencyGroupStatusCmd struct {
	Group string `arg:"" optional:"" help:"Concurrency group key. Omit to list every group."`
}

func (cmd *ConcurrencyGroupStatusCmd) Run(app *Context) error {
	if cmd.Group == "" {
		groups, err := app.Client.ListConcurrencyGroups(app.Context)
		if err != nil {
			return fmt.Errorf("list concurrency groups: %w", err)
		}
		return app.Print(groups)
	}
	group, err := app.Client.GetConcurrencyGroup(app.Context, cmd.Group)
	if err != nil {
		return fmt.Errorf("get concurrency group: %w", err)
	}
	return app.Print(group)
}

func (app *Context) printConcurrencyGroups(groups []buildkite.ConcurrencyGroup) error {
	if _, err := fmt.Fprintf(app.Output, "CONCURRENCY GROUP STATUS\nGroups: %d\n\n", len(groups)); err != nil {
		return err
	}
	if len(groups) == 0 {
		_, err := fmt.Fprintln(app.Output, "No concurrency groups found.")
		return err
	}
	for i, group := range groups {
		if i > 0 {
			if _, err := fmt.Fprintln(app.Output); err != nil {
				return err
			}
		}
		blockingQueues := strings.Join(group.BlockingQueues, ", ")
		if blockingQueues == "" {
			blockingQueues = "none"
		}
		destination := group.DestinationClusterID
		if destination == "" {
			destination = "—"
		}
		if _, err := fmt.Fprintf(app.Output, "Group: %s\nState: %s\nDestination cluster: %s\nRunning source jobs: %d\nWaiting jobs: %d\nBlocking queues: %s\n", group.Key, group.State, destination, group.RunningSourceJobs, group.WaitingJobs, blockingQueues); err != nil {
			return err
		}
		if group.URL != "" {
			if _, err := fmt.Fprintf(app.Output, "URL: %s\n", group.URL); err != nil {
				return err
			}
		}
	}
	return nil
}
