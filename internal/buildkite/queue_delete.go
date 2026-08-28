package buildkite

import (
	"context"
	"net/http"
)

func (c *Client) DeleteQueueMigration(ctx context.Context, queue string) error {
	return c.do(ctx, http.MethodDelete, c.queuePath(queue), nil, nil)
}
