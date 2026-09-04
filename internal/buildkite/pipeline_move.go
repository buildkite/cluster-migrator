package buildkite

import (
	"context"
	"fmt"
	"net/http"
)

func (c *Client) MovePipeline(ctx context.Context, pipeline, clusterID string) (*Pipeline, error) {
	request := struct {
		ClusterID string `json:"cluster_id"`
	}{ClusterID: clusterID}

	var result Pipeline
	path := c.path("cluster-queue-migrations", "pipelines", pipeline, "move")
	if err := c.do(ctx, http.MethodPost, path, request, &result); err != nil {
		return nil, err
	}
	if result.ClusterID != clusterID {
		return nil, fmt.Errorf("move pipeline response cluster_id %q does not match destination %q", result.ClusterID, clusterID)
	}
	return &result, nil
}
