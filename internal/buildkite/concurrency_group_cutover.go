package buildkite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// ConcurrencyGroupCutoverBlocked is the server's 409 response when a cutover cannot start.
// Nothing is changed when a cutover is rejected.
type ConcurrencyGroupCutoverBlocked struct {
	Message              string                           `json:"message"`
	Scope                string                           `json:"scope"`
	Key                  string                           `json:"key"`
	DestinationClusterID string                           `json:"destination_cluster_id"`
	Ready                bool                             `json:"ready"`
	Status               string                           `json:"status"`
	Migration            ConcurrencyGroupCutoverMigration `json:"migration"`
	QueueObservation     ConcurrencyGroupQueueObservation `json:"queue_observation"`
}

func (e *ConcurrencyGroupCutoverBlocked) Error() string {
	return e.Message
}

type ConcurrencyGroupCutoverMigration struct {
	State          string  `json:"state"`
	BlockingReason *string `json:"blocking_reason"`
}

type ConcurrencyGroupQueueObservation struct {
	ObservedAt      *string                         `json:"observed_at"`
	WindowStartedAt *string                         `json:"window_started_at"`
	WindowSeconds   int                             `json:"window_seconds"`
	Queues          []string                        `json:"queues"`
	BlockingQueues  []ConcurrencyGroupBlockingQueue `json:"blocking_queues"`
}

type ConcurrencyGroupBlockingQueue struct {
	Queue            string   `json:"queue"`
	Reasons          []string `json:"reasons"`
	RoutedPercent    *int     `json:"routed_percent"`
	RecentSourceJobs int      `json:"recent_source_jobs"`
}

// StartConcurrencyGroupCutover asks the server to hold, drain, and move an unclustered group into the cluster.
// The server checks readiness first; a blocked cutover returns *ConcurrencyGroupCutoverBlocked.
func (c *Client) StartConcurrencyGroupCutover(ctx context.Context, scope, key string) (*ConcurrencyGroup, error) {
	var group ConcurrencyGroup
	err := c.do(ctx, http.MethodPost, c.concurrencyGroupPath(scope, key, "cutover"), nil, &group)
	var apiError *APIError
	if errors.As(err, &apiError) && apiError.StatusCode == http.StatusConflict {
		var blocked ConcurrencyGroupCutoverBlocked
		if json.Unmarshal(apiError.Body, &blocked) == nil && blocked.Status != "" {
			return nil, &blocked
		}
	}
	if err != nil {
		return nil, err
	}
	if group.State == "" {
		return nil, fmt.Errorf("cutover response is missing the migration state")
	}
	return &group, nil
}
