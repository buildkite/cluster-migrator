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
		if app.JSON {
			return app.Print(groups)
		}
		clusterNames, err := concurrencyGroupClusterNames(app)
		if err != nil {
			return err
		}
		return app.printConcurrencyGroups(groups, clusterNames)
	}
	group, err := app.Client.GetConcurrencyGroup(app.Context, cmd.Group)
	if err != nil {
		return fmt.Errorf("get concurrency group: %w", err)
	}
	if app.JSON {
		return app.Print(group)
	}
	clusterNames, err := concurrencyGroupClusterNames(app)
	if err != nil {
		return err
	}
	return app.printConcurrencyGroupStatus(group, clusterNames)
}

func concurrencyGroupClusterNames(app *Context) (map[string]string, error) {
	clusters, err := app.Client.ListClusters(app.Context)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}
	names := make(map[string]string, len(clusters))
	for _, cluster := range clusters {
		names[cluster.ID] = cluster.Name
	}
	return names, nil
}

func concurrencyGroupDestination(group *buildkite.ConcurrencyGroup, clusterNames map[string]string) string {
	if name := clusterNames[group.DestinationClusterID]; name != "" {
		return name
	}
	return displayValue(group.DestinationClusterID)
}

func (app *Context) printConcurrencyGroups(groups []buildkite.ConcurrencyGroup, clusterNames map[string]string) error {
	if _, err := fmt.Fprintf(app.Output, "CONCURRENCY-GROUP STATUS\nGroups: %d\n\nSTATUS\n\n", len(groups)); err != nil {
		return err
	}
	if len(groups) == 0 {
		_, err := fmt.Fprintln(app.Output, "No concurrency groups found.")
		return err
	}
	writer := tabwriter.NewWriter(app.Output, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "GROUP\tSTATE\tDESTINATION\tRUNNING SOURCE JOBS\tWAITING JOBS\tBLOCKING QUEUES\tURL")
	for i := range groups {
		group := &groups[i]
		_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\t%d\t%d\t%s\t%s\n",
			displayValue(group.Key),
			displayValue(group.State),
			concurrencyGroupDestination(group, clusterNames),
			group.RunningSourceJobs,
			group.WaitingJobs,
			displayValue(strings.Join(group.BlockingQueues, ", ")),
			displayValue(group.URL),
		)
	}
	return writer.Flush()
}

func (app *Context) printConcurrencyGroupStatus(group *buildkite.ConcurrencyGroup, clusterNames map[string]string) error {
	if _, err := fmt.Fprintf(app.Output, "CONCURRENCY-GROUP STATUS\nGroup: %s\nDestination: %s\n", group.Key, concurrencyGroupDestination(group, clusterNames)); err != nil {
		return err
	}
	if err := app.printConcurrencyGroupState(group); err != nil {
		return err
	}
	return app.printNextSteps(concurrencyGroupStatusNextSteps(group))
}

func (app *Context) printConcurrencyGroupState(group *buildkite.ConcurrencyGroup) error {
	if _, err := fmt.Fprintf(app.Output, "\nSTATUS\n\nState: %s\nRunning source jobs: %d\nWaiting jobs: %d\n", displayValue(group.State), group.RunningSourceJobs, group.WaitingJobs); err != nil {
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

func concurrencyGroupStatusNextSteps(group *buildkite.ConcurrencyGroup) []string {
	destination := group.DestinationClusterID
	if destination == "" {
		destination = "<cluster>"
	}
	if len(group.BlockingQueues) > 0 {
		return concurrencyGroupBlockedNextSteps(group.Key, group.BlockingQueues, destination)
	}
	switch group.State {
	case "unclustered", "blocked":
		return []string{fmt.Sprintf("Preview the cutover:\n\n   cluster-migrator concurrency-group cutover %s --destination-cluster %s --dry-run", group.Key, destination)}
	case "clustered", "succeeded":
		return concurrencyGroupCompletedNextSteps(destination)
	default:
		return nil
	}
}

func concurrencyGroupBlockedNextSteps(group string, queues []string, destination string) []string {
	commands := make([]string, len(queues))
	for i, queue := range queues {
		commands[i] = "   cluster-migrator queue status " + queue
	}
	return []string{
		"Review the migration for each blocking queue:\n\n" + strings.Join(commands, "\n"),
		fmt.Sprintf("If no queue blockers remain, preview the cutover:\n\n   cluster-migrator concurrency-group cutover %s --destination-cluster %s --dry-run", group, destination),
	}
}

func concurrencyGroupCompletedNextSteps(destination string) []string {
	return []string{fmt.Sprintf("Assess each pipeline using this concurrency group:\n\n   cluster-migrator pipeline readiness <pipeline> --destination-cluster %s", destination)}
}
