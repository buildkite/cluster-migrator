package buildkite

import (
	"context"
	"net/http"
	"net/url"
)

func (c *Client) GetPipelineReadiness(ctx context.Context, pipeline, clusterID string) (*PipelineReadiness, error) {
	path := c.path("cluster-migration", "pipelines", pipeline, "readiness")
	query := url.Values{"destination_cluster_id": []string{clusterID}}

	var result PipelineReadiness
	if err := c.do(ctx, http.MethodGet, path+"?"+query.Encode(), nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
