package cli

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"text/tabwriter"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

type ConcurrencyGroupCutoverCmd struct {
	Group string `arg:"" help:"Concurrency group as <scope>/<key>, such as custom/deploy."`
}

func (cmd *ConcurrencyGroupCutoverCmd) Run(app *Context) error {
	scope, key, err := parseConcurrencyGroup(cmd.Group)
	if err != nil {
		return err
	}
	migration, err := app.Client.GetMigration(app.Context)
	if err != nil {
		return fmt.Errorf("get organization migration: %w", err)
	}
	cluster, err := app.Client.ResolveCluster(app.Context, migration.DestinationClusterID)
	if err != nil {
		return fmt.Errorf("resolve destination cluster: %w", err)
	}
	if app.DryRun {
		return cmd.dryRun(app, cluster, scope, key)
	}

	// The server checks readiness and rejects a blocked cutover without changing anything.
	group, err := app.Client.StartConcurrencyGroupCutover(app.Context, scope, key)
	var blocked *buildkite.ConcurrencyGroupCutoverBlocked
	if errors.As(err, &blocked) {
		if app.JSON {
			if err := app.Print(blocked); err != nil {
				return err
			}
		} else if err := cmd.printBlocked(app, cluster, blocked); err != nil {
			return err
		}
		return errConcurrencyGroupCutoverBlocked
	}
	if err != nil {
		return fmt.Errorf("start concurrency-group cutover: %w", err)
	}
	if app.JSON {
		return app.Print(group)
	}
	return cmd.printResult(app, cluster, group)
}

func (cmd *ConcurrencyGroupCutoverCmd) printHeading(app *Context, destination string) error {
	heading := "CONCURRENCY-GROUP CUTOVER"
	if app.DryRun {
		heading += " (DRY RUN)"
	}
	_, err := fmt.Fprintf(app.Output, "%s\nGroup: %s\nDestination: %s\n", heading, cmd.Group, destination)
	return err
}

var errConcurrencyGroupCutoverBlocked = errors.New("concurrency group has known cutover blockers")

// dryRun checks only the group's migration state; queue checks run when a cutover is requested.
func (cmd *ConcurrencyGroupCutoverCmd) dryRun(app *Context, cluster *buildkite.Cluster, scope, key string) error {
	migration, err := app.Client.GetConcurrencyGroup(app.Context, scope, key)
	var apiError *buildkite.APIError
	if errors.As(err, &apiError) && apiError.StatusCode == http.StatusNotFound {
		migration, err = nil, nil
	}
	if err != nil {
		return fmt.Errorf("get concurrency group: %w", err)
	}
	blocker := concurrencyGroupMigrationBlockerFor(migration, cluster.ID)

	if app.JSON {
		if blocker != "" {
			return fmt.Errorf("concurrency group cannot cut over: %s", concurrencyGroupMigrationBlocker(blocker))
		}
		return app.Print(Change{Action: "cut over", Resource: "concurrency group " + cmd.Group, DestinationCluster: cluster.Name, DryRun: true})
	}

	if err := cmd.printHeading(app, cluster.Name); err != nil {
		return err
	}
	if blocker != "" {
		if _, err := fmt.Fprintf(app.Output, "\nREADINESS\n\nCutover is blocked by the concurrency group's migration.\n\nState: %s\nBlocker: %s\n",
			concurrencyGroupState(migration.State),
			concurrencyGroupMigrationBlocker(blocker),
		); err != nil {
			return err
		}
		if err := app.printNextSteps(concurrencyGroupMigrationBlockerActions(cmd.Group)); err != nil {
			return err
		}
		return errConcurrencyGroupCutoverBlocked
	}

	state := "No migration has started for this concurrency group."
	if migration != nil {
		state = "The concurrency group is unclustered and can cut over."
	}
	_, err = fmt.Fprintf(app.Output, "\nREADINESS\n\n%s Queue routing and recent ungrouped jobs are checked when the cutover is requested.\n\nPROPOSED CHANGE\n\nA cutover to the %s cluster would be requested.\n", state, cluster.Name)
	return err
}

// concurrencyGroupMigrationBlockerFor mirrors the server's rule: only a group with no migration, or a stable
// unclustered one for the same destination, can start a cutover.
func concurrencyGroupMigrationBlockerFor(migration *buildkite.ConcurrencyGroup, clusterID string) string {
	if migration == nil {
		return ""
	}
	switch migration.State {
	case buildkite.ConcurrencyGroupUnclustered:
		if migration.DestinationClusterID != clusterID {
			return "wrong_destination"
		}
		return ""
	case buildkite.ConcurrencyGroupForwardDraining, buildkite.ConcurrencyGroupForwardReconciling:
		return "cutover_in_progress"
	case buildkite.ConcurrencyGroupClustered:
		return "already_clustered"
	default:
		return displayValue(migration.State)
	}
}

func concurrencyGroupMigrationBlockerActions(group string) []string {
	return []string{fmt.Sprintf("Check the concurrency group's migration:\n\n   cluster-migrator concurrency-group status %s", group)}
}

func (cmd *ConcurrencyGroupCutoverCmd) printBlocked(app *Context, cluster *buildkite.Cluster, blocked *buildkite.ConcurrencyGroupCutoverBlocked) error {
	if err := cmd.printHeading(app, cluster.Name); err != nil {
		return err
	}
	count := concurrencyGroupCutoverBlockerCount(blocked)
	if _, err := fmt.Fprintf(app.Output, "\nRESULT\n\nCutover rejected without changes. Blocked by %d known %s.\n", count, pluralize(count, "condition")); err != nil {
		return err
	}
	if reason := blocked.Migration.BlockingReason; reason != nil {
		if _, err := fmt.Fprintf(app.Output, "\nMIGRATION BLOCKER\n\nState: %s\nBlocker: %s\n", concurrencyGroupState(blocked.Migration.State), concurrencyGroupMigrationBlocker(*reason)); err != nil {
			return err
		}
	}
	if err := app.printConcurrencyGroupQueueBlockers(blocked.QueueObservation.BlockingQueues); err != nil {
		return err
	}
	if err := app.printConcurrencyGroupQueueObservation(blocked.QueueObservation); err != nil {
		return err
	}
	return app.printNextSteps(concurrencyGroupCutoverActions(cmd.Group, blocked))
}

func (cmd *ConcurrencyGroupCutoverCmd) printResult(app *Context, cluster *buildkite.Cluster, group *buildkite.ConcurrencyGroup) error {
	if err := cmd.printHeading(app, cluster.Name); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(app.Output, "\nRESULT\n\nCutover to the %s cluster started.\n", cluster.Name); err != nil {
		return err
	}
	if err := app.printConcurrencyGroupState(group); err != nil {
		return err
	}
	return app.printNextSteps([]string{fmt.Sprintf("Check cutover progress:\n\n   cluster-migrator concurrency-group status %s", cmd.Group)})
}

func concurrencyGroupCutoverBlockerCount(readiness *buildkite.ConcurrencyGroupCutoverBlocked) int {
	count := 0
	if readiness.Migration.BlockingReason != nil {
		count++
	}
	for _, blocker := range readiness.QueueObservation.BlockingQueues {
		count += max(1, len(blocker.Reasons))
	}
	return count
}

func (app *Context) printConcurrencyGroupQueueBlockers(blockers []buildkite.ConcurrencyGroupBlockingQueue) error {
	if len(blockers) == 0 {
		return nil
	}
	if _, err := fmt.Fprint(app.Output, "\nQUEUE BLOCKERS\n\n"); err != nil {
		return err
	}
	writer := tabwriter.NewWriter(app.Output, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "QUEUE\tROUTING\tRECENT UNGROUPED JOBS\tBLOCKER")
	for _, blocker := range blockers {
		reasons := blocker.Reasons
		if len(reasons) == 0 {
			reasons = []string{""}
		}
		for _, reason := range reasons {
			_, _ = fmt.Fprintf(writer, "%s\t%s\t%d\t%s\n",
				displayValue(blocker.Queue),
				metricPercent(blocker.RoutedPercent),
				blocker.RecentSourceJobs,
				concurrencyGroupQueueBlocker(reason),
			)
		}
	}
	return writer.Flush()
}

func (app *Context) printConcurrencyGroupQueueObservation(observation buildkite.ConcurrencyGroupQueueObservation) error {
	queues := "none observed"
	if len(observation.Queues) > 0 {
		queues = strings.Join(observation.Queues, ", ")
	}
	window := pipelineObservationWindow(observation.WindowSeconds)
	if observation.ObservedAt != nil && *observation.ObservedAt != "" {
		if freshness, err := formatMetricsFreshness(*observation.ObservedAt, app.now()); err == nil {
			window += " ending " + freshness
		}
	}
	if observation.WindowStartedAt != nil && *observation.WindowStartedAt != "" {
		window += "; started " + pipelineObservationTimestamp(*observation.WindowStartedAt)
	}
	_, err := fmt.Fprintf(app.Output, "\nOBSERVATIONS\n\nQueues used by this group: %s\nRecent jobs: %s\n", queues, window)
	return err
}

func concurrencyGroupMigrationBlocker(reason string) string {
	return describeBlocker(reason, map[string]string{
		"cutover_in_progress": "A cutover is already in progress.",
		"already_clustered":   "The concurrency group has already moved to a cluster.",
		"wrong_destination":   "The concurrency group's migration targets another cluster.",
	})
}

func concurrencyGroupQueueBlocker(reason string) string {
	return describeBlocker(reason, map[string]string{
		"migration_missing":  "No queue migration is configured.",
		"routing_incomplete": "Routing is below 100%.",
		"wrong_destination":  "The queue migration targets another cluster.",
		"recent_source_jobs": "Ungrouped jobs were created on the unclustered queue recently.",
	})
}

func concurrencyGroupCutoverActions(group string, readiness *buildkite.ConcurrencyGroupCutoverBlocked) []string {
	var actions []string
	seen := make(map[string]bool)
	appendAction := func(action string) {
		if !seen[action] {
			actions = append(actions, action)
			seen[action] = true
		}
	}
	if readiness.Migration.BlockingReason != nil {
		for _, action := range concurrencyGroupMigrationBlockerActions(group) {
			appendAction(action)
		}
	}
	for _, blocker := range readiness.QueueObservation.BlockingQueues {
		if blocker.Queue == "" {
			continue
		}
		for _, reason := range blocker.Reasons {
			switch reason {
			case "migration_missing":
				appendAction(fmt.Sprintf("Inspect the organization migration's phase before configuring the missing cluster queue migration for %s:\n\n   cluster-migrator migration status", blocker.Queue))
			case "routing_incomplete":
				appendAction(fmt.Sprintf("Review destination activity for %s before increasing routing:\n\n   cluster-migrator queue metrics %s", blocker.Queue, blocker.Queue))
			case "recent_source_jobs":
				appendAction(fmt.Sprintf("Wait until no ungrouped jobs have been created on the unclustered %s queue for 10 minutes, then retry the cutover:\n\n   cluster-migrator concurrency-group cutover %s", blocker.Queue, group))
			}
		}
	}
	return actions
}
