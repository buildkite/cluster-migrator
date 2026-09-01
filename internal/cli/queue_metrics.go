package cli

import (
	"context"
	"errors"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

const queueMetricsTimeout = time.Minute

type QueueMetricsCmd struct {
	Queue string `arg:"" help:"Source queue key."`
}

func (cmd *QueueMetricsCmd) Run(app *Context) error {
	ctx, cancel := context.WithTimeout(app.Context, queueMetricsTimeout)
	defer cancel()
	retries := 0
	retryMessagePrinted := false

	for {
		metrics, err := app.Client.GetQueueMetrics(ctx, cmd.Queue)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return fmt.Errorf("queue metrics did not become available within %s", queueMetricsTimeout)
			}
			return fmt.Errorf("get queue metrics: %w", err)
		}
		if metrics.RetryAfter == nil {
			return app.Print(metrics)
		}

		retryAfter := *metrics.RetryAfter
		if retryAfter <= 0 {
			return fmt.Errorf("invalid retry_after_seconds: %d", retryAfter)
		}
		if retries > 0 && !retryMessagePrinted {
			if _, err := fmt.Fprintf(app.ErrorOutput, "Queue metrics are still being prepared; retrying every %d %s…\n", retryAfter, pluralize(retryAfter, "second")); err != nil {
				return err
			}
			retryMessagePrinted = true
		}
		retries++
		if err := app.wait(ctx, time.Duration(retryAfter)*time.Second); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return fmt.Errorf("queue metrics did not become available within %s", queueMetricsTimeout)
			}
			return fmt.Errorf("wait to retry queue metrics: %w", err)
		}
	}
}

func (c *Context) printQueueMetrics(metrics *buildkite.QueueMetrics) error {
	freshness := "—"
	if metrics.ObservedAt != nil {
		var err error
		freshness, err = formatMetricsFreshness(*metrics.ObservedAt, c.now())
		if err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(c.Output, "QUEUE ACTIVITY\nSource: %s (unclustered)\nDestination: %s (cluster %s)\nRouting: %s\nObserved: %s\n\n",
		displayValue(metrics.Queue),
		displayValue(metrics.Destination.QueueKey),
		displayValue(metrics.Destination.ClusterID),
		metricPercent(metrics.RoutedPercent),
		freshness,
	); err != nil {
		return err
	}
	writer := tabwriter.NewWriter(c.Output, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintf(writer, "METRIC\tLATEST\t%dM MAX\n", metrics.WindowSeconds/60)
	_, _ = fmt.Fprintf(writer, "Connected agents\t%s\t%s\n", metricValue(metrics.Activity.ConnectedAgents.Current), metricValue(metrics.Activity.ConnectedAgents.Peak))
	_, _ = fmt.Fprintf(writer, "Waiting jobs\t%s\t%s\n", metricValue(metrics.Activity.WaitingJobs.Current), metricValue(metrics.Activity.WaitingJobs.Peak))
	_, _ = fmt.Fprintf(writer, "Running jobs\t%s\t%s\n", metricValue(metrics.Activity.RunningJobs.Current), metricValue(metrics.Activity.RunningJobs.Peak))
	return writer.Flush()
}

func displayValue(value string) string {
	if value == "" {
		return "—"
	}
	return value
}

func metricPercent(value *int) string {
	if value == nil {
		return "—"
	}
	return fmt.Sprintf("%d%%", *value)
}

func metricValue(value *int) string {
	if value == nil {
		return "—"
	}
	return fmt.Sprintf("%d", *value)
}

func formatMetricsFreshness(observedAt string, now time.Time) (string, error) {
	observed, err := time.Parse(time.RFC3339Nano, observedAt)
	if err != nil {
		return "", fmt.Errorf("parse queue metrics observed_at: %w", err)
	}
	elapsed := now.Sub(observed)
	if elapsed < 0 {
		elapsed = 0
	}

	switch {
	case elapsed < time.Second:
		return "just now", nil
	case elapsed < time.Minute:
		seconds := int(elapsed / time.Second)
		return fmt.Sprintf("%d %s ago", seconds, pluralize(seconds, "second")), nil
	case elapsed < time.Hour:
		minutes := int(elapsed / time.Minute)
		return fmt.Sprintf("%d %s ago", minutes, pluralize(minutes, "minute")), nil
	default:
		hours := int(elapsed / time.Hour)
		return fmt.Sprintf("%d %s ago", hours, pluralize(hours, "hour")), nil
	}
}

func pluralize(value int, unit string) string {
	if value == 1 {
		return unit
	}
	return unit + "s"
}
