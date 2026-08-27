package buildkite

import (
	"context"
	"net/http"
)

func (c *Client) SetQueueMigrationPercent(ctx context.Context, queue string, percent int) (*QueueMigration, error) {
	request := struct {
		RoutedPercent int `json:"routed_percent"`
	}{RoutedPercent: percent}

	var migration QueueMigration
	if err := c.do(ctx, http.MethodPost, c.queuePath(queue)+"/route", request, &migration); err != nil {
		return nil, err
	}
	return &migration, nil
}
