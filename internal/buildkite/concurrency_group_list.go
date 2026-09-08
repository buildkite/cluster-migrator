package buildkite

import "context"

func (c *Client) ListConcurrencyGroups(ctx context.Context) ([]ConcurrencyGroup, error) {
	path := c.path("cluster-queue-migrations", "concurrency-groups")
	groups := []ConcurrencyGroup{}
	for path != "" {
		page, err := cursorPage[ConcurrencyGroup](ctx, c, path)
		if err != nil {
			return nil, err
		}
		groups = append(groups, page.Items...)
		path = page.Links.Next
	}
	return groups, nil
}
