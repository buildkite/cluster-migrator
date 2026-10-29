package buildkite

import (
	"context"
	"fmt"
	"net/http"
)

func (c *Client) MovePipeline(ctx context.Context, pipeline, clusterID string) (*Pipeline, error) {
	var result Pipeline
	path := c.path("unclustered-to-clustered", "migration", "pipelines", pipeline)
	if err := c.do(ctx, http.MethodPatch, path, nil, &result); err != nil {
		return nil, err
	}
	if result.ClusterID != clusterID {
		return nil, fmt.Errorf("move pipeline response cluster_id %q does not match destination %q", result.ClusterID, clusterID)
	}
	return &result, nil
}
