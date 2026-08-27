package buildkite

import (
	"context"
	"net/http"
)

func (c *Client) ListConcurrencyGroups(ctx context.Context) ([]ConcurrencyGroup, error) {
	var response struct {
		Items []ConcurrencyGroup `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, c.path("cluster-queue-migrations", "concurrency-groups"), nil, &response); err != nil {
		return nil, err
	}
	return response.Items, nil
}
