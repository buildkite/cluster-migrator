package buildkite

import (
	"context"
	"net/http"
)

type QueueMetrics struct {
	Queue           string           `json:"queue"`
	Destination     QueueDestination `json:"destination"`
	RoutedPercent   *int             `json:"routed_percent"`
	Status          string           `json:"status"`
	WindowStartedAt *string          `json:"window_started_at"`
	ObservedAt      *string          `json:"observed_at"`
	WindowSeconds   int              `json:"window_seconds"`
	Activity        QueueActivity    `json:"activity"`
}

type QueueActivity struct {
	ConnectedAgents MetricValues `json:"connected_agents"`
	WaitingJobs     MetricValues `json:"waiting_jobs"`
	RunningJobs     MetricValues `json:"running_jobs"`
}

type MetricValues struct {
	Current *int `json:"current"`
	Peak    *int `json:"peak"`
}

func (c *Client) GetQueueMetrics(ctx context.Context, queue string) (*QueueMetrics, error) {
	var metrics QueueMetrics
	if err := c.do(ctx, http.MethodGet, c.queuePath(queue)+"/metrics", nil, &metrics); err != nil {
		return nil, err
	}
	return &metrics, nil
}
