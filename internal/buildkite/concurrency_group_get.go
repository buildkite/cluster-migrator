package buildkite

import (
	"context"
	"net/http"
)

func (c *Client) GetConcurrencyGroup(ctx context.Context, group string) (*ConcurrencyGroup, error) {
	var result ConcurrencyGroup
	if err := c.do(ctx, http.MethodGet, c.concurrencyGroupPath(group), nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
