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
	if err := c.do(ctx, http.MethodPatch, c.pipelinePath(pipeline), request, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
