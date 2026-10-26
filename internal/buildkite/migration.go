package buildkite

import (
	"context"
	"net/http"
)

type Migration struct {
	ID                   string `json:"id"`
	OrganizationID       string `json:"organization_id"`
	DestinationClusterID string `json:"destination_cluster_id"`
	Phase                string `json:"phase"`
	CreatedByID          string `json:"created_by_id"`
	URL                  string `json:"url"`
	CreatedAt            string `json:"created_at"`
	UpdatedAt            string `json:"updated_at"`
}

func (c *Client) ConfigureMigration(ctx context.Context, destinationClusterID string) (*Migration, error) {
	request := struct {
		DestinationClusterID string `json:"destination_cluster_id"`
	}{DestinationClusterID: destinationClusterID}

	var migration Migration
	if err := c.do(ctx, http.MethodPost, c.path("unclustered-to-clustered-migration"), request, &migration); err != nil {
		return nil, err
	}
	return &migration, nil
}

func (c *Client) GetMigration(ctx context.Context) (*Migration, error) {
	var migration Migration
	if err := c.do(ctx, http.MethodGet, c.path("unclustered-to-clustered-migration"), nil, &migration); err != nil {
		return nil, err
	}
	return &migration, nil
}

func (c *Client) ValidateQueues(ctx context.Context) (*Migration, error) {
	return c.changeQueuePhase(ctx, "validate-queues")
}

func (c *Client) PrepareQueues(ctx context.Context) (*Migration, error) {
	return c.changeQueuePhase(ctx, "prepare-queues")
}

func (c *Client) changeQueuePhase(ctx context.Context, operation string) (*Migration, error) {
	var migration Migration
	if err := c.do(ctx, http.MethodPost, c.path("unclustered-to-clustered-migration", operation), nil, &migration); err != nil {
		return nil, err
	}
	return &migration, nil
}
