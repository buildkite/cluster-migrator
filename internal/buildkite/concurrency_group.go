package buildkite

import (
	"context"
	"net/http"
)

const (
	ConcurrencyGroupUnclustered        = "unclustered"
	ConcurrencyGroupForwardDraining    = "forward_draining"
	ConcurrencyGroupForwardReconciling = "forward_reconciling"
	ConcurrencyGroupClustered          = "clustered"
)

// ConcurrencyGroup is a concurrency group's migration. One exists only once a cutover has started.
type ConcurrencyGroup struct {
	Scope                string                      `json:"scope"`
	Key                  string                      `json:"key"`
	State                string                      `json:"state"`
	DestinationClusterID string                      `json:"destination_cluster_id"`
	HeldAt               *string                     `json:"held_at"`
	SourceJobs           *ConcurrencyGroupSourceJobs `json:"source_jobs"`
	URL                  string                      `json:"url"`
	CreatedAt            string                      `json:"created_at"`
	UpdatedAt            string                      `json:"updated_at"`
}

// ConcurrencyGroupSourceJobs counts a group's unfinished unclustered jobs. Collections omit it.
type ConcurrencyGroupSourceJobs struct {
	Running int `json:"running"`
	Waiting int `json:"waiting"`
}

// A concurrency group is identified by its scope and key; equal keys in different scopes are distinct groups.
// Each is a single path segment, so a slash in a key is sent as %2F.
func (c *Client) concurrencyGroupPath(scope, key string, parts ...string) string {
	segments := append([]string{"unclustered-to-clustered", "concurrency-group-migrations", scope, key}, parts...)
	return c.path(segments...)
}

// ListConcurrencyGroups returns every concurrency-group migration in the organization.
func (c *Client) ListConcurrencyGroups(ctx context.Context) ([]ConcurrencyGroup, error) {
	path := c.path("unclustered-to-clustered", "concurrency-group-migrations") + "?per_page=100"
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

func (c *Client) GetConcurrencyGroup(ctx context.Context, scope, key string) (*ConcurrencyGroup, error) {
	var group ConcurrencyGroup
	if err := c.do(ctx, http.MethodGet, c.concurrencyGroupPath(scope, key), nil, &group); err != nil {
		return nil, err
	}
	return &group, nil
}
