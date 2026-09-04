package buildkite

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

type QueueMetrics struct {
	Queue         string                   `json:"queue"`
	RoutedPercent *int                     `json:"routed_percent"`
	RetryAfter    *int                     `json:"retry_after_seconds"`
	Destination   *QueueMetricsDestination `json:"destination"`
	Source        *QueueSource             `json:"source"`
}

type QueueMetricsDestination struct {
	ClusterID       string         `json:"cluster_id"`
	QueueID         string         `json:"queue_id"`
	WindowStartedAt *string        `json:"window_started_at"`
	ObservedAt      *string        `json:"observed_at"`
	NextRefreshAt   *string        `json:"next_refresh_at"`
	WindowSeconds   int            `json:"window_seconds"`
	Activity        *QueueActivity `json:"activity"`
}

type QueueActivity struct {
	ConnectedAgents    MetricValues        `json:"connected_agents"`
	WaitingJobs        MetricValues        `json:"waiting_jobs"`
	RunningJobs        MetricValues        `json:"running_jobs"`
	WaitTimeP95Seconds DecimalMetricValues `json:"wait_time_p95_seconds"`
}

type QueueSource struct {
	ObservedAt    *string              `json:"observed_at"`
	NextRefreshAt *string              `json:"next_refresh_at"`
	Activity      *QueueSourceActivity `json:"activity"`
}

type QueueSourceActivity struct {
	ConnectedAgents MetricValues `json:"connected_agents"`
	WaitingJobs     MetricValues `json:"waiting_jobs"`
	RunningJobs     MetricValues `json:"running_jobs"`
}

type MetricValues struct {
	Current *int `json:"current"`
	Peak    *int `json:"peak"`
}

type DecimalMetricValues struct {
	Current *float64 `json:"current"`
	Peak    *float64 `json:"peak"`
}

func (c *Client) GetQueueMetrics(ctx context.Context, queue string) (*QueueMetrics, error) {
	var metrics QueueMetrics
	if err := c.do(ctx, http.MethodGet, c.queuePath(queue)+"/metrics", nil, &metrics); err != nil {
		return nil, err
	}
	if metrics.Destination == nil {
		return nil, errors.New("queue metrics response missing required destination")
	}
	if metrics.Source == nil {
		return nil, errors.New("queue metrics response missing required source")
	}
	if metrics.Destination.Activity == nil {
		return nil, errors.New("queue metrics response missing required destination activity")
	}
	if metrics.Source.Activity == nil {
		return nil, errors.New("queue metrics response missing required source activity")
	}
	if metrics.Destination.NextRefreshAt == nil {
		return nil, errors.New("queue metrics response missing required destination next_refresh_at")
	}
	if metrics.Source.ObservedAt == nil {
		return nil, errors.New("queue metrics response missing required source observed_at")
	}
	if metrics.Source.NextRefreshAt == nil {
		return nil, errors.New("queue metrics response missing required source next_refresh_at")
	}
	if metrics.Destination.ObservedAt != nil {
		if _, err := time.Parse(time.RFC3339Nano, *metrics.Destination.ObservedAt); err != nil {
			return nil, fmt.Errorf("parse destination observed_at: %w", err)
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, *metrics.Destination.NextRefreshAt); err != nil {
		return nil, fmt.Errorf("parse destination next_refresh_at: %w", err)
	}
	if _, err := time.Parse(time.RFC3339Nano, *metrics.Source.ObservedAt); err != nil {
		return nil, fmt.Errorf("parse source observed_at: %w", err)
	}
	if _, err := time.Parse(time.RFC3339Nano, *metrics.Source.NextRefreshAt); err != nil {
		return nil, fmt.Errorf("parse source next_refresh_at: %w", err)
	}
	return &metrics, nil
}
