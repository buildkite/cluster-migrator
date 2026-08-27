package buildkite

import (
	"context"
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
	return &result, nil
}
