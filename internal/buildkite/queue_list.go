package buildkite

import "context"

func (c *Client) ListQueueMigrations(ctx context.Context) ([]QueueMigration, *bool, error) {
	path := c.path("unclustered-to-clustered", "queue-migrations")
	migrations := []QueueMigration{}
	var configured *bool
	firstPage := true
	for path != "" {
		page, err := cursorPage[QueueMigration](ctx, c, path)
		if err != nil {
			return nil, nil, err
		}
		if firstPage {
			configured = page.MigrationConfigured
			firstPage = false
		}
		migrations = append(migrations, page.Items...)
		path = page.Links.Next
	}
	return migrations, configured, nil
}
