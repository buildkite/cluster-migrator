package buildkite

import (
	"context"
	"net/http"
)

func (c *Client) ListQueueMigrations(ctx context.Context) ([]QueueMigration, error) {
	var response struct {
		Items []QueueMigration `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, c.path("cluster-queue-migrations"), nil, &response); err != nil {
		return nil, err
	}
	return response.Items, nil
}
