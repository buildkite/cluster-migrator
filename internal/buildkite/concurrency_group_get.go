package buildkite

import (
	"context"
	"net/http"
)

func (c *Client) GetConcurrencyGroup(ctx context.Context, scope, key string) (*ConcurrencyGroup, error) {
	var result ConcurrencyGroup
	if err := c.do(ctx, http.MethodGet, c.concurrencyGroupPath(scope, key), nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
