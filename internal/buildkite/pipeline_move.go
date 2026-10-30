package buildkite

import (
	"context"
	"fmt"
	"net/http"
)

func (c *Client) MovePipeline(ctx context.Context, pipeline string) (*Pipeline, error) {
	var result Pipeline
	path := c.path("unclustered-to-clustered", "migration", "pipelines", pipeline)
	if err := c.do(ctx, http.MethodPatch, path, nil, &result); err != nil {
		return nil, err
	}
	if result.ClusterID == "" {
		return nil, fmt.Errorf("move pipeline response missing cluster_id")
	}
	return &result, nil
}
