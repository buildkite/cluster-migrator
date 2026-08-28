package buildkite

import (
	"context"
	"net/http"
)

func (c *Client) GetPipeline(ctx context.Context, pipeline string) (*Pipeline, error) {
	var result Pipeline
	if err := c.do(ctx, http.MethodGet, c.pipelinePath(pipeline), nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
