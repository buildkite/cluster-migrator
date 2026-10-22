package buildkite

import (
	"context"
	"net/http"
)

type OrganizationMigration struct {
	ID                   string `json:"id"`
	OrganizationID       string `json:"organization_id"`
	DestinationClusterID string `json:"destination_cluster_id"`
	Phase                string `json:"phase"`
	CreatedByID          string `json:"created_by_id"`
	URL                  string `json:"url"`
	CreatedAt            string `json:"created_at"`
	UpdatedAt            string `json:"updated_at"`
}

func (c *Client) GetMigration(ctx context.Context) (*OrganizationMigration, error) {
	var migration OrganizationMigration
	if err := c.do(ctx, http.MethodGet, c.path("unclustered-to-clustered-migration"), nil, &migration); err != nil {
		return nil, err
	}
	return &migration, nil
}

func (c *Client) ConfigureMigration(ctx context.Context, destinationClusterID string) (*OrganizationMigration, error) {
	request := struct {
		DestinationClusterID string `json:"destination_cluster_id"`
	}{DestinationClusterID: destinationClusterID}
	var migration OrganizationMigration
	if err := c.do(ctx, http.MethodPut, c.path("unclustered-to-clustered-migration"), request, &migration); err != nil {
		return nil, err
	}
	return &migration, nil
}
