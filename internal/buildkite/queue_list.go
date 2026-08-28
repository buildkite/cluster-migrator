package buildkite

import "context"

func (c *Client) ListQueueMigrations(ctx context.Context) ([]QueueMigration, error) {
	path := c.path("cluster-queue-migrations")
	var migrations []QueueMigration
	for path != "" {
		page, err := cursorPage[QueueMigration](ctx, c, path)
		if err != nil {
			return nil, err
		}
		migrations = append(migrations, page.Items...)
		path = page.Links.Next
	}
	return migrations, nil
}
