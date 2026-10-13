package cli

import (
	"errors"
	"fmt"
	"net/http"
	"text/tabwriter"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

type ConcurrencyGroupStatusCmd struct {
	Group string `arg:"" optional:"" help:"Concurrency group as <scope>/<key>, such as custom/deploy. Omit to list every migration."`
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
		clusterNames, err := concurrencyGroupClusterNames(app, len(groups) > 0)
		if err != nil {
			return err
		}
		return app.printConcurrencyGroups(groups, clusterNames)
	}

	scope, key, err := parseConcurrencyGroup(cmd.Group)
	if err != nil {
		return err
	}
	group, err := app.Client.GetConcurrencyGroup(app.Context, scope, key)
	var apiError *buildkite.APIError
	if errors.As(err, &apiError) && apiError.StatusCode == http.StatusNotFound {
		// Migrations exist only once a cutover starts, so a missing group usually just hasn't been cut over.
		return fmt.Errorf("no migration has started for concurrency group %s", cmd.Group)
	}
	if err != nil {
		return fmt.Errorf("get concurrency group: %w", err)
	}
	if app.JSON {
		return app.Print(group)
	}
	clusterNames, err := concurrencyGroupClusterNames(app, true)
	if err != nil {
		return err
	}
	return app.printConcurrencyGroupStatus(group, clusterNames)
}

// concurrencyGroupClusterNames resolves destination names, skipping the request when there is nothing to show.
func concurrencyGroupClusterNames(app *Context, needed bool) (map[string]string, error) {
	if !needed {
		return nil, nil
	}
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

func concurrencyGroupHeldSince(group *buildkite.ConcurrencyGroup) string {
	if group.HeldAt == nil || *group.HeldAt == "" {
		return "—"
	}
	return pipelineObservationTimestamp(*group.HeldAt)
}

// concurrencyGroupState explains transitional states; stable states speak for themselves.
func concurrencyGroupState(state string) string {
	description := ""
	switch state {
	case buildkite.ConcurrencyGroupClustered:
		return state
	case buildkite.ConcurrencyGroupUnclustered:
		// Migrations start held, so an unheld unclustered migration has been reversed.
		description = "moved back to unclustered queues"
	case buildkite.ConcurrencyGroupForwardDraining:
		description = "cutover is waiting for running unclustered jobs to finish"
	case buildkite.ConcurrencyGroupForwardReconciling:
		description = "cutover is moving waiting jobs to the destination"
	case buildkite.ConcurrencyGroupReverseDraining:
		description = "reversal is waiting for running cluster jobs to finish"
	case buildkite.ConcurrencyGroupReverseReconciling:
		description = "reversal is moving waiting jobs back to unclustered queues"
	default:
		description = "unknown state"
	}
	return fmt.Sprintf("%s (%s)", displayValue(state), description)
}

func (app *Context) printConcurrencyGroups(groups []buildkite.ConcurrencyGroup, clusterNames map[string]string) error {
	if _, err := fmt.Fprintf(app.Output, "CONCURRENCY-GROUP STATUS\nMigrations: %d\n\nSTATUS\n\n", len(groups)); err != nil {
		return err
	}
	if len(groups) == 0 {
		_, err := fmt.Fprintln(app.Output, "No concurrency-group migrations found.")
		return err
	}
	writer := tabwriter.NewWriter(app.Output, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "GROUP\tSTATE\tDESTINATION\tHELD SINCE")
	for i := range groups {
		group := &groups[i]
		_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n",
			concurrencyGroupName(group),
			displayValue(group.State),
			concurrencyGroupDestination(group, clusterNames),
			concurrencyGroupHeldSince(group),
		)
	}
	return writer.Flush()
}

func (app *Context) printConcurrencyGroupStatus(group *buildkite.ConcurrencyGroup, clusterNames map[string]string) error {
	if _, err := fmt.Fprintf(app.Output, "CONCURRENCY-GROUP STATUS\nGroup: %s\nDestination: %s\n",
		concurrencyGroupName(group),
		concurrencyGroupDestination(group, clusterNames),
	); err != nil {
		return err
	}
	if err := app.printConcurrencyGroupState(group); err != nil {
		return err
	}
	return app.printNextSteps(concurrencyGroupStatusNextSteps(group))
}

func (app *Context) printConcurrencyGroupState(group *buildkite.ConcurrencyGroup) error {
	if _, err := fmt.Fprintf(app.Output, "\nSTATUS\n\nState: %s\n", concurrencyGroupState(group.State)); err != nil {
		return err
	}
	if group.HeldAt != nil && *group.HeldAt != "" {
		if _, err := fmt.Fprintf(app.Output, "Held since: %s\n", concurrencyGroupHeldSince(group)); err != nil {
			return err
		}
	}
	if group.SourceJobs != nil {
		if _, err := fmt.Fprintf(app.Output, "Running unclustered jobs: %d\nWaiting unclustered jobs: %d\n", group.SourceJobs.Running, group.SourceJobs.Waiting); err != nil {
			return err
		}
	}
	return nil
}

func concurrencyGroupStatusNextSteps(group *buildkite.ConcurrencyGroup) []string {
	if group.State == buildkite.ConcurrencyGroupClustered {
		return []string{fmt.Sprintf("Assess each pipeline using this concurrency group:\n\n   cluster-migrator pipeline readiness <pipeline> --destination-cluster %s", group.DestinationClusterID)}
	}
	return nil
}

func (app *Context) printNextSteps(steps []string) error {
	if len(steps) == 0 {
		return nil
	}
	if _, err := fmt.Fprint(app.Output, "\nNEXT STEPS\n\n"); err != nil {
		return err
	}
	for index, step := range steps {
		if index > 0 {
			if _, err := fmt.Fprintln(app.Output); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(app.Output, "%d. %s\n", index+1, step); err != nil {
			return err
		}
	}
	return nil
}
