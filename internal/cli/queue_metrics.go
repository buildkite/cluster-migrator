package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
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
				return queueMetricsDeadlineError(app.Context)
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
				return queueMetricsDeadlineError(app.Context)
			}
			return fmt.Errorf("wait to retry queue metrics: %w", err)
		}
	}
}

func queueMetricsDeadlineError(parent context.Context) error {
	if err := parent.Err(); err != nil {
		return err
	}
	return fmt.Errorf("queue metrics did not become available within %s", queueMetricsTimeout)
}

func (c *Context) printQueueMetrics(metrics *buildkite.QueueMetrics) error {
	now := c.now()
	destinationFreshness := "—"
	if metrics.Destination.ObservedAt != nil {
		var err error
		destinationFreshness, err = formatCompactMetricsFreshness(*metrics.Destination.ObservedAt, now)
		if err != nil {
			return err
		}
	}

	destinationRefreshDue, err := formatCompactMetricsRefreshDue(*metrics.Destination.NextRefreshAt, now)
	if err != nil {
		return err
	}

	sourceFreshness, err := formatCompactMetricsFreshness(*metrics.Source.ObservedAt, now)
	if err != nil {
		return err
	}
	sourceRefreshDue, err := formatCompactMetricsRefreshDue(*metrics.Source.NextRefreshAt, now)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(c.Output, "QUEUE METRICS\nQueue: %s\nRouting: %s\nDestination: %s\n\nACTIVITY\n\n",
		displayValue(metrics.Queue),
		metricPercent(metrics.RoutedPercent),
		displayValue(metrics.Destination.ClusterID),
	); err != nil {
		return err
	}
	writer := tabwriter.NewWriter(c.Output, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintf(writer, "METRIC\tSOURCE LATEST\tDEST LATEST\tDEST %s\n", metricsWindowHeading(metrics.Destination.WindowSeconds))
	_, _ = fmt.Fprintf(writer, "Waiting jobs\t%s\t%s\t%s\n", metricValue(metrics.Source.Activity.WaitingJobs.Current), metricValue(metrics.Destination.Activity.WaitingJobs.Current), metricValue(metrics.Destination.Activity.WaitingJobs.Peak))
	_, _ = fmt.Fprintf(writer, "Running jobs\t%s\t%s\t%s\n", metricValue(metrics.Source.Activity.RunningJobs.Current), metricValue(metrics.Destination.Activity.RunningJobs.Current), metricValue(metrics.Destination.Activity.RunningJobs.Peak))
	_, _ = fmt.Fprintf(writer, "Connected agents\t%s\t%s\t%s\n", metricValue(metrics.Source.Activity.ConnectedAgents.Current), metricValue(metrics.Destination.Activity.ConnectedAgents.Current), metricValue(metrics.Destination.Activity.ConnectedAgents.Peak))
	_, _ = fmt.Fprintf(writer, "Wait time (p95)\t—\t%s\t%s\n", metricDuration(metrics.Destination.Activity.WaitTimeP95Seconds.Current), metricDuration(metrics.Destination.Activity.WaitTimeP95Seconds.Peak))
	if err := writer.Flush(); err != nil {
		return err
	}

	_, err = fmt.Fprintf(c.Output, "\nOBSERVATIONS\n\nSource observed: %s\nSource refresh eligible: %s\nDestination observed: %s\nDestination refresh eligible: %s\n",
		sourceFreshness,
		sourceRefreshDue,
		destinationFreshness,
		destinationRefreshDue,
	)
	return err
}

func metricsWindowHeading(windowSeconds int) string {
	if windowSeconds > 0 && windowSeconds%60 == 0 {
		return fmt.Sprintf("%dM MAX", windowSeconds/60)
	}
	return fmt.Sprintf("%dS MAX", windowSeconds)
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

func metricDuration(value *float64) string {
	if value == nil {
		return "—"
	}
	return strconv.FormatFloat(*value, 'f', -1, 64) + "s"
}

func formatCompactMetricsFreshness(observedAt string, now time.Time) (string, error) {
	observed, err := time.Parse(time.RFC3339Nano, observedAt)
	if err != nil {
		return "", fmt.Errorf("parse queue metrics observed_at: %w", err)
	}
	elapsed := now.Sub(observed)
	if elapsed < time.Second {
		return "just now", nil
	}
	return formatCompactDuration(elapsed) + " ago", nil
}

func formatCompactMetricsRefreshDue(nextRefreshAt string, now time.Time) (string, error) {
	refreshAt, err := time.Parse(time.RFC3339Nano, nextRefreshAt)
	if err != nil {
		return "", fmt.Errorf("parse queue metrics next_refresh_at: %w", err)
	}
	remaining := refreshAt.Sub(now)
	if remaining <= 0 {
		return "now", nil
	}
	if remaining < time.Second {
		return "in <1s", nil
	}
	return "in " + formatCompactDuration(remaining), nil
}

func formatCompactDuration(duration time.Duration) string {
	totalSeconds := int(duration / time.Second)
	hours := totalSeconds / 3600
	minutes := totalSeconds / 60 % 60
	seconds := totalSeconds % 60
	parts := make([]string, 0, 3)
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	if seconds > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%ds", seconds))
	}
	return strings.Join(parts, " ")
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
		seconds := int(elapsed/time.Second) % 60
		if seconds == 0 {
			return fmt.Sprintf("%d %s ago", minutes, pluralize(minutes, "minute")), nil
		}
		return fmt.Sprintf("%d %s %d %s ago", minutes, pluralize(minutes, "minute"), seconds, pluralize(seconds, "second")), nil
	default:
		hours := int(elapsed / time.Hour)
		return fmt.Sprintf("%d %s ago", hours, pluralize(hours, "hour")), nil
	}
}

func formatMetricsRefreshDue(nextRefreshAt string, now time.Time) (string, error) {
	refreshAt, err := time.Parse(time.RFC3339Nano, nextRefreshAt)
	if err != nil {
		return "", fmt.Errorf("parse queue metrics next_refresh_at: %w", err)
	}
	if !refreshAt.After(now) {
		return "now", nil
	}

	remainingSeconds := int(refreshAt.Sub(now) / time.Second)
	if remainingSeconds == 0 {
		return "in less than 1 second", nil
	}
	minutes := remainingSeconds / 60
	seconds := remainingSeconds % 60
	if minutes == 0 {
		return fmt.Sprintf("in %d %s", seconds, pluralize(seconds, "second")), nil
	}
	if seconds == 0 {
		return fmt.Sprintf("in %d %s", minutes, pluralize(minutes, "minute")), nil
	}
	return fmt.Sprintf("in %d %s %d %s", minutes, pluralize(minutes, "minute"), seconds, pluralize(seconds, "second")), nil
}

func pluralize(value int, unit string) string {
	if value == 1 {
		return unit
	}
	return unit + "s"
}
