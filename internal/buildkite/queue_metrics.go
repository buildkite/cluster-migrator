package buildkite

import (
	"context"
	"errors"
	"net/http"
)

type QueueMetrics struct {
	Queue           string           `json:"queue"`
	Destination     QueueDestination `json:"destination"`
	RoutedPercent   *int             `json:"routed_percent"`
	RetryAfter      *int             `json:"retry_after_seconds"`
	WindowStartedAt *string          `json:"window_started_at"`
	ObservedAt      *string          `json:"observed_at"`
	NextRefreshAt   *string          `json:"next_refresh_at,omitempty"`
	WindowSeconds   int              `json:"window_seconds"`
	Activity        QueueActivity    `json:"activity"`
	Source          *QueueSource     `json:"source"`
}

type QueueActivity struct {
	ConnectedAgents    MetricValues        `json:"connected_agents"`
	WaitingJobs        MetricValues        `json:"waiting_jobs"`
	RunningJobs        MetricValues        `json:"running_jobs"`
	WaitTimeP95Seconds DecimalMetricValues `json:"wait_time_p95_seconds"`
}

type QueueSource struct {
	QueueKey   string              `json:"queue_key"`
	ObservedAt *string             `json:"observed_at"`
	Activity   QueueSourceActivity `json:"activity"`
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
	if metrics.Source == nil {
		return nil, errors.New("queue metrics response missing required source")
	}
	return &metrics, nil
}
