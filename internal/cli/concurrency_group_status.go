package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

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
	if _, err := fmt.Fprintf(app.Output, "CONCURRENCY-GROUP STATUS\nGroups: %d\n\nSTATUS\n\n", len(groups)); err != nil {
		return err
	}
	if len(groups) == 0 {
		_, err := fmt.Fprintln(app.Output, "No concurrency groups found.")
		return err
	}
	writer := tabwriter.NewWriter(app.Output, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "GROUP\tSTATE\tDESTINATION\tRUNNING SOURCE\tWAITING\tBLOCKING QUEUES\tURL")
	for _, group := range groups {
		row := []string{group.Key, group.State, group.DestinationClusterID, fmt.Sprint(group.RunningSourceJobs), fmt.Sprint(group.WaitingJobs), strings.Join(group.BlockingQueues, ", "), group.URL}
		for i, value := range row {
			if value == "" {
				row[i] = "—"
			}
		}
		_, _ = fmt.Fprintln(writer, strings.Join(row, "\t"))
	}
	return writer.Flush()
}

func (app *Context) printConcurrencyGroupStatus(group *buildkite.ConcurrencyGroup) error {
	if _, err := fmt.Fprintf(app.Output, "CONCURRENCY-GROUP STATUS\nGroup: %s\n", group.Key); err != nil {
		return err
	}
	return app.printConcurrencyGroupState(group)
}

func (app *Context) printConcurrencyGroupState(group *buildkite.ConcurrencyGroup) error {
	destination := group.DestinationClusterID
	if destination == "" {
		destination = "—"
	}
	if _, err := fmt.Fprintf(app.Output, "\nSTATUS\n\nState: %s\nDestination cluster: %s\nRunning source jobs: %d\nWaiting jobs: %d\n", group.State, destination, group.RunningSourceJobs, group.WaitingJobs); err != nil {
		return err
	}
	if group.URL != "" {
		if _, err := fmt.Fprintf(app.Output, "URL: %s\n", group.URL); err != nil {
			return err
		}
	}
	return app.printConcurrencyGroupBlockingQueues(group.BlockingQueues)
}

func (app *Context) printConcurrencyGroupBlockingQueues(queues []string) error {
	if len(queues) == 0 {
		return nil
	}
	_, err := fmt.Fprintf(app.Output, "\nBLOCKING QUEUES\n\nQUEUE\n%s\n", strings.Join(queues, "\n"))
	return err
}
