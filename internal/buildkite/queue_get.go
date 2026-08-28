package buildkite

import (
	"context"
	"net/http"
)

func (c *Client) GetQueueMigration(ctx context.Context, queue string) (*QueueMigration, error) {
	var migration QueueMigration
	if err := c.do(ctx, http.MethodGet, c.queuePath(queue), nil, &migration); err != nil {
		return nil, err
	}
	return &migration, nil
}
