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
	return c.transitionMigration(ctx, "validate-queues", nil)
}

func (c *Client) PrepareQueues(ctx context.Context) (*Migration, error) {
	return c.transitionMigration(ctx, "prepare-queues", nil)
}

func (c *Client) CutoverConcurrencyGroups(ctx context.Context, acknowledgeIrreversible bool) (*Migration, error) {
	request := struct {
		AcknowledgeIrreversible bool `json:"acknowledge_irreversible"`
	}{AcknowledgeIrreversible: acknowledgeIrreversible}
	return c.transitionMigration(ctx, "cutover-concurrency-groups", request)
}

func (c *Client) CutoverPipelines(ctx context.Context) (*Migration, error) {
	return c.transitionMigration(ctx, "cutover-pipelines", nil)
}

func (c *Client) CompleteMigration(ctx context.Context, finalizeBlockedBuilds bool) (*Migration, error) {
	request := struct {
		FinalizeBlockedBuilds bool `json:"finalize_blocked_builds"`
	}{FinalizeBlockedBuilds: finalizeBlockedBuilds}
	return c.transitionMigration(ctx, "complete", request)
}

func (c *Client) transitionMigration(ctx context.Context, operation string, request any) (*Migration, error) {
	var migration Migration
	if err := c.do(ctx, http.MethodPost, c.path("unclustered-to-clustered-migration", operation), request, &migration); err != nil {
		return nil, err
	}
	return &migration, nil
}
