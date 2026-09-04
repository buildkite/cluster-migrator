package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

const pipelineReadinessTimeout = time.Minute

type PipelineReadinessCmd struct {
	Pipeline           string `arg:"" help:"Pipeline ID, name, or slug."`
	DestinationCluster string `name:"destination-cluster" required:"" help:"Destination cluster name or ID."`
}

func (cmd *PipelineReadinessCmd) Run(app *Context) error {
	cluster, err := app.Client.ResolveCluster(app.Context, cmd.DestinationCluster)
	if err != nil {
		return fmt.Errorf("resolve destination cluster: %w", err)
	}
	readiness, err := getPipelineReadinessForPipeline(app, cmd.Pipeline, cluster.ID)
	if err != nil {
		return err
	}
	if err := app.Print(readiness); err != nil {
		return err
	}
	if readiness.Status == buildkite.PipelineReadinessBlocked {
		return fmt.Errorf("pipeline has known blockers")
	}
	if readiness.Status != buildkite.PipelineReadinessNoKnownBlockers {
		return fmt.Errorf("unknown pipeline assessment status %q", readiness.Status)
	}
	return nil
}

func getPipelineReadinessForPipeline(app *Context, pipeline, clusterID string) (*buildkite.PipelineReadiness, error) {
	return runWithPipelineIdentifierFallback(app.Context, app.Client, pipeline, func(pipeline string) (*buildkite.PipelineReadiness, error) {
		return getPipelineReadiness(app, pipeline, clusterID)
	})
}

func getPipelineReadiness(app *Context, pipeline, clusterID string) (*buildkite.PipelineReadiness, error) {
	ctx, cancel := context.WithTimeout(app.Context, pipelineReadinessTimeout)
	defer cancel()
	retries := 0
	retryMessagePrinted := false

	for {
		readiness, err := app.Client.GetPipelineReadiness(ctx, pipeline, clusterID)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, pipelineReadinessDeadlineError(app.Context)
			}
			return nil, fmt.Errorf("get pipeline readiness: %w", err)
		}
		if readiness.Status != buildkite.PipelineReadinessPending {
			return readiness, nil
		}
		if readiness.RetryAfter == nil || *readiness.RetryAfter <= 0 {
			retryAfter := 0
			if readiness.RetryAfter != nil {
				retryAfter = *readiness.RetryAfter
			}
			return nil, fmt.Errorf("invalid retry_after_seconds: %d", retryAfter)
		}

		retryAfter := *readiness.RetryAfter
		if retries > 0 && !retryMessagePrinted {
			if _, err := fmt.Fprintf(app.ErrorOutput, "Pipeline assessment is still being prepared; retrying every %d %s…\n", retryAfter, pluralize(retryAfter, "second")); err != nil {
				return nil, err
			}
			retryMessagePrinted = true
		}
		retries++
		if err := app.wait(ctx, time.Duration(retryAfter)*time.Second); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, pipelineReadinessDeadlineError(app.Context)
			}
			return nil, fmt.Errorf("wait to retry pipeline assessment: %w", err)
		}
	}
}

func pipelineReadinessDeadlineError(parent context.Context) error {
	if err := parent.Err(); err != nil {
		return err
	}
	return fmt.Errorf("pipeline assessment did not become available within %s", pipelineReadinessTimeout)
}

func (c *Context) printPipelineReadiness(readiness *buildkite.PipelineReadiness) error {
	blockerCount := len(readiness.QueueObservation.BlockingQueues) + len(readiness.ConcurrencyGroupObservation.BlockingConcurrencyGroups)
	if _, err := fmt.Fprintf(c.Output, "PIPELINE READINESS\nPipeline: %s\nDestination cluster: %s\nStatus: %s\n\n",
		displayValue(readiness.Pipeline),
		displayValue(readiness.DestinationClusterID),
		pipelineReadinessStatus(readiness.Status, blockerCount),
	); err != nil {
		return err
	}

	if err := c.printPipelineQueueBlockers(readiness.QueueObservation.BlockingQueues); err != nil {
		return err
	}
	if err := c.printPipelineConcurrencyGroupBlockers(readiness.ConcurrencyGroupObservation.BlockingConcurrencyGroups); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(c.Output, "OBSERVATIONS\nQueues: %s\nConcurrency groups: %s\n",
		c.pipelineObservationSummary(readiness.QueueObservation.Complete, readiness.QueueObservation.WindowSeconds, readiness.QueueObservation.ObservedAt, readiness.QueueObservation.WindowStartedAt),
		c.pipelineObservationSummary(readiness.ConcurrencyGroupObservation.Complete, readiness.ConcurrencyGroupObservation.WindowSeconds, readiness.ConcurrencyGroupObservation.ObservedAt, readiness.ConcurrencyGroupObservation.WindowStartedAt),
	); err != nil {
		return err
	}
	if !readiness.QueueObservation.Complete || !readiness.ConcurrencyGroupObservation.Complete {
		if _, err := fmt.Fprint(c.Output, "\nIncomplete observations may omit blockers outside the observed window.\n"); err != nil {
			return err
		}
	}

	return c.printPipelineReadinessActions(readiness)
}

func pipelineReadinessStatus(status string, blockerCount int) string {
	switch status {
	case buildkite.PipelineReadinessBlocked:
		return fmt.Sprintf("BLOCKED — %d known %s", blockerCount, pluralize(blockerCount, "blocker"))
	case buildkite.PipelineReadinessNoKnownBlockers:
		return "NO KNOWN BLOCKERS — not proof of readiness"
	default:
		return fmt.Sprintf("UNKNOWN (%s)", displayValue(status))
	}
}

func (c *Context) printPipelineQueueBlockers(blockers []buildkite.PipelineBlockingQueue) error {
	if len(blockers) == 0 {
		_, err := fmt.Fprint(c.Output, "QUEUE BLOCKERS\nNone\n\n")
		return err
	}

	if _, err := fmt.Fprintf(c.Output, "QUEUE BLOCKERS (%d)\n\n", len(blockers)); err != nil {
		return err
	}
	writer := tabwriter.NewWriter(c.Output, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "QUEUE\tROUTING\tACTIVE SOURCE JOBS")
	for _, blocker := range blockers {
		_, _ = fmt.Fprintf(writer, "%s\t%s\t%d\n", displayValue(blocker.Queue), metricPercent(blocker.RoutedPercent), blocker.ActiveSourceJobs)
	}
	if err := writer.Flush(); err != nil {
		return err
	}

	for _, blocker := range blockers {
		if _, err := fmt.Fprintf(c.Output, "\n%s\n", displayValue(blocker.Queue)); err != nil {
			return err
		}
		if len(blocker.Reasons) == 0 {
			if _, err := fmt.Fprintln(c.Output, "  - Unknown blocker (reason unavailable)"); err != nil {
				return err
			}
			continue
		}
		for _, reason := range blocker.Reasons {
			if _, err := fmt.Fprintf(c.Output, "  - %s\n", pipelineBlockerReason(reason)); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintln(c.Output)
	return err
}

func (c *Context) printPipelineConcurrencyGroupBlockers(blockers []buildkite.BlockingConcurrencyGroup) error {
	if len(blockers) == 0 {
		_, err := fmt.Fprint(c.Output, "CONCURRENCY-GROUP BLOCKERS\nNone\n\n")
		return err
	}

	if _, err := fmt.Fprintf(c.Output, "CONCURRENCY-GROUP BLOCKERS (%d)\n\n", len(blockers)); err != nil {
		return err
	}
	writer := tabwriter.NewWriter(c.Output, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "SCOPE\tKEY")
	for _, blocker := range blockers {
		_, _ = fmt.Fprintf(writer, "%s\t%s\n", displayValue(blocker.Scope), displayValue(blocker.Key))
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	for _, blocker := range blockers {
		if _, err := fmt.Fprintf(c.Output, "\n%s / %s\n  - %s\n",
			displayValue(blocker.Scope),
			displayValue(blocker.Key),
			pipelineBlockerReason(blocker.Reason),
		); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(c.Output)
	return err
}

func pipelineBlockerReason(reason string) string {
	var description string
	switch reason {
	case "routing_incomplete":
		description = "Routing is below 100%"
	case "migration_missing":
		description = "No queue migration is configured"
	case "active_source_jobs":
		description = "Active jobs remain on the source queue"
	case "concurrency_group_migration_unavailable":
		description = "Migration is unavailable for this concurrency group"
	default:
		description = "Unknown blocker"
	}
	if reason == "" {
		return description + " (reason unavailable)"
	}
	return fmt.Sprintf("%s (%s)", description, reason)
}

func (c *Context) pipelineObservationSummary(complete bool, windowSeconds int, observedAt, windowStartedAt *string) string {
	parts := []string{"incomplete"}
	if complete {
		parts[0] = "complete"
	}
	parts = append(parts, pipelineObservationWindow(windowSeconds))
	if observedAt == nil || *observedAt == "" {
		parts = append(parts, "observation time unavailable")
	} else if freshness, err := formatMetricsFreshness(*observedAt, c.now()); err == nil {
		parts[len(parts)-1] += " ending " + freshness
	} else {
		parts[len(parts)-1] += " ending at " + *observedAt
	}
	if windowStartedAt != nil && *windowStartedAt != "" {
		parts = append(parts, "started "+pipelineObservationTimestamp(*windowStartedAt))
	}
	return strings.Join(parts, "; ")
}

func pipelineObservationWindow(seconds int) string {
	if seconds <= 0 {
		return "window unavailable"
	}
	if seconds%60 == 0 {
		minutes := seconds / 60
		return fmt.Sprintf("%d-minute window", minutes)
	}
	return fmt.Sprintf("%d-second window", seconds)
}

func pipelineObservationTimestamp(value string) string {
	timestamp, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	return timestamp.UTC().Format("2006-01-02 15:04:05 UTC")
}

func (c *Context) printPipelineReadinessActions(readiness *buildkite.PipelineReadiness) error {
	actions := pipelineReadinessActions(readiness)
	if len(actions) == 0 {
		return nil
	}
	if _, err := fmt.Fprint(c.Output, "\nNEXT STEPS\n\n"); err != nil {
		return err
	}
	for index, action := range actions {
		if index > 0 {
			if _, err := fmt.Fprintln(c.Output); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(c.Output, "%d. %s\n", index+1, action); err != nil {
			return err
		}
	}
	return nil
}

func pipelineReadinessActions(readiness *buildkite.PipelineReadiness) []string {
	var actions []string
	seen := make(map[string]bool)
	appendAction := func(action string) {
		if !seen[action] {
			actions = append(actions, action)
			seen[action] = true
		}
	}
	for _, blocker := range readiness.QueueObservation.BlockingQueues {
		for _, reason := range blocker.Reasons {
			switch reason {
			case "routing_incomplete":
				if blocker.Queue != "" {
					appendAction(fmt.Sprintf("Review destination activity for %s:\n\n   cluster-migrator queue metrics %s", blocker.Queue, blocker.Queue))
				}
			case "migration_missing":
				if blocker.Queue != "" && readiness.DestinationClusterID != "" {
					appendAction(fmt.Sprintf("Configure the missing queue migration for %s:\n\n   cluster-migrator queue configure %s --destination-cluster %s", blocker.Queue, blocker.Queue, readiness.DestinationClusterID))
				}
			case "active_source_jobs":
				if blocker.Queue != "" && readiness.Pipeline != "" && readiness.DestinationClusterID != "" {
					appendAction(fmt.Sprintf("Wait for active source jobs on %s to finish, then reassess:\n\n   cluster-migrator pipeline readiness %s --destination-cluster %s", blocker.Queue, readiness.Pipeline, readiness.DestinationClusterID))
				}
			}
		}
	}
	return actions
}
