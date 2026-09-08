package buildkite

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type PipelineRollback struct {
	Pipeline             string                    `json:"pipeline"`
	ClusterID            *string                   `json:"cluster_id"`
	AssignmentChanged    bool                      `json:"assignment_changed"`
	Cutoff               string                    `json:"cutoff"`
	ScannedThrough       string                    `json:"scanned_through"`
	Passes               int                       `json:"passes"`
	Selected             int                       `json:"selected"`
	CancellationEnqueued int                       `json:"cancellation_enqueued"`
	Pending              int                       `json:"pending"`
	Failures             []PipelineRollbackFailure `json:"failures"`
	BestEffort           bool                      `json:"best_effort"`
}

type PipelineRollbackFailure struct {
	BuildUUID string `json:"build_uuid"`
	Message   string `json:"message"`
}

func (c *Client) RollbackPipeline(ctx context.Context, pipeline string) (*PipelineRollback, error) {
	var response json.RawMessage
	path := c.path("cluster-queue-migrations", "pipelines", pipeline, "rollback")
	if err := c.do(ctx, http.MethodPost, path, struct{}{}, &response); err != nil {
		return nil, err
	}
	var result PipelineRollback
	if err := json.Unmarshal(response, &result); err != nil {
		return nil, fmt.Errorf("decode rollback response: %w", err)
	}
	// Missing counts or booleans must not turn an incomplete response into success.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(response, &fields); err != nil {
		return nil, fmt.Errorf("decode rollback response fields: %w", err)
	}
	for _, field := range []string{"pipeline", "assignment_changed", "cutoff", "scanned_through", "passes", "selected", "cancellation_enqueued", "pending", "failures", "best_effort"} {
		if value, ok := fields[field]; !ok || string(value) == "null" {
			return nil, fmt.Errorf("rollback response missing %s", field)
		}
	}
	if string(fields["cluster_id"]) != "null" {
		return nil, fmt.Errorf("rollback response cluster_id must be null")
	}
	cutoff, err := time.Parse(time.RFC3339Nano, result.Cutoff)
	if err != nil {
		return nil, fmt.Errorf("parse rollback cutoff: %w", err)
	}
	scannedThrough, err := time.Parse(time.RFC3339Nano, result.ScannedThrough)
	if err != nil {
		return nil, fmt.Errorf("parse rollback scanned_through: %w", err)
	}
	if result.Pipeline == "" || !result.BestEffort || result.Passes != 2 || scannedThrough.Before(cutoff) {
		return nil, fmt.Errorf("invalid rollback response metadata")
	}
	if result.Selected < 0 || result.CancellationEnqueued < 0 || result.CancellationEnqueued > result.Selected || result.Pending < 0 {
		return nil, fmt.Errorf("invalid rollback response counts")
	}
	return &result, nil
}
