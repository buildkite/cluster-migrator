package buildkite

import (
	"context"
	"net/http"
)

func (c *Client) ListQueueMigrations(ctx context.Context) ([]QueueMigration, error) {
	path := c.path("cluster-queue-migrations")
	var migrations []QueueMigration
	for path != "" {
		var response struct {
			Items []QueueMigration `json:"items"`
			Links struct {
				Next string `json:"next"`
			} `json:"links"`
		}
		if err := c.do(ctx, http.MethodGet, path, nil, &response); err != nil {
			return nil, err
		}
		migrations = append(migrations, response.Items...)
		path = response.Links.Next
	}
	return migrations, nil
}
