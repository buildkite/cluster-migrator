package buildkite

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

func (c *Client) GetPipelineReadiness(ctx context.Context, pipeline, clusterID string) (*PipelineReadiness, error) {
	path := c.path("cluster-queue-migrations", "pipelines", pipeline, "readiness")
	query := url.Values{"destination_cluster_id": []string{clusterID}}

	var result PipelineReadiness
	if err := c.do(ctx, http.MethodGet, path+"?"+query.Encode(), nil, &result); err != nil {
		return nil, err
	}
	if result.Status != PipelineReadinessBlocked && result.Status != PipelineReadinessNoKnownBlockers {
		return &result, nil
	}
	if result.QueueObservation.NextRefreshAt == nil {
		return nil, errors.New("pipeline readiness response missing required queue_observation.next_refresh_at")
	}
	if result.ConcurrencyGroupObservation.NextRefreshAt == nil {
		return nil, errors.New("pipeline readiness response missing required concurrency_group_observation.next_refresh_at")
	}
	if _, err := time.Parse(time.RFC3339Nano, *result.QueueObservation.NextRefreshAt); err != nil {
		return nil, fmt.Errorf("parse queue_observation.next_refresh_at: %w", err)
	}
	if _, err := time.Parse(time.RFC3339Nano, *result.ConcurrencyGroupObservation.NextRefreshAt); err != nil {
		return nil, fmt.Errorf("parse concurrency_group_observation.next_refresh_at: %w", err)
	}
	return &result, nil
}
