package buildkite

import (
	"context"
	"net/http"
	"net/url"
)

type Migration struct {
	ID                     string `json:"id"`
	OrganizationID         string `json:"organization_id"`
	DestinationClusterID   string `json:"destination_cluster_id"`
	Phase                  string `json:"phase"`
	CreatedByID            string `json:"created_by_id"`
	URL                    string `json:"url"`
	CreatedAt              string `json:"created_at"`
	UpdatedAt              string `json:"updated_at"`
	DestinationClusterName string `json:"destination_cluster_name,omitempty"`
	NextPhase              string `json:"next_phase,omitempty"`
}

type MigrationReadiness struct {
	Phase                         string             `json:"phase"`
	TargetPhase                   *string            `json:"target_phase"`
	Ready                         bool               `json:"ready"`
	Status                        string             `json:"status"`
	BlockerSamples                []MigrationBlocker `json:"blocker_samples"`
	BlockerSampleLimitPerCategory int                `json:"blocker_sample_limit_per_category"`
	NextPhase                     string             `json:"next_phase,omitempty"`
	CheckedAt                     string             `json:"checked_at,omitempty"`
}

type MigrationBlocker struct {
	Code    string  `json:"code"`
	Message string  `json:"message"`
	Queue   *string `json:"queue"`
	Scope   *string `json:"scope"`
	Key     *string `json:"key"`
}

func (c *Client) ConfigureMigration(ctx context.Context, destinationClusterID string) (*Migration, error) {
	request := struct {
		DestinationClusterID string `json:"destination_cluster_id"`
	}{DestinationClusterID: destinationClusterID}

	var migration Migration
	if err := c.do(ctx, http.MethodPost, c.path("unclustered-to-clustered", "migration"), request, &migration); err != nil {
		return nil, err
	}
	return &migration, nil
}

func (c *Client) GetMigration(ctx context.Context) (*Migration, error) {
	return c.getMigration(ctx, "")
}

func (c *Client) GetMigrationSummary(ctx context.Context) (*Migration, error) {
	return c.getMigration(ctx, "?include=summary")
}

func (c *Client) getMigration(ctx context.Context, query string) (*Migration, error) {
	var migration Migration
	if err := c.do(ctx, http.MethodGet, c.path("unclustered-to-clustered", "migration")+query, nil, &migration); err != nil {
		return nil, err
	}
	return &migration, nil
}

func (c *Client) GetMigrationReadiness(ctx context.Context) (*MigrationReadiness, error) {
	return c.getMigrationReadiness(ctx, "")
}

func (c *Client) GetMigrationTransitionReadiness(ctx context.Context, targetPhase string) (*MigrationReadiness, error) {
	return c.getMigrationReadiness(ctx, "?"+url.Values{"target_phase": {targetPhase}}.Encode())
}

func (c *Client) getMigrationReadiness(ctx context.Context, query string) (*MigrationReadiness, error) {
	var readiness MigrationReadiness
	if err := c.do(ctx, http.MethodGet, c.path("unclustered-to-clustered", "migration", "readiness")+query, nil, &readiness); err != nil {
		return nil, err
	}
	return &readiness, nil
}

func (c *Client) ValidateQueues(ctx context.Context) (*Migration, error) {
	return c.transitionMigration(ctx, "validate-queues")
}

func (c *Client) PrepareQueues(ctx context.Context) (*Migration, error) {
	return c.transitionMigration(ctx, "prepare-queues")
}

func (c *Client) CutoverConcurrencyGroups(ctx context.Context) (*Migration, error) {
	return c.transitionMigration(ctx, "cutover-concurrency-groups")
}

func (c *Client) CutoverPipelines(ctx context.Context) (*Migration, error) {
	return c.transitionMigration(ctx, "cutover-pipelines")
}

func (c *Client) CompleteMigration(ctx context.Context) (*Migration, error) {
	return c.transitionMigration(ctx, "complete")
}

func (c *Client) FinalizeBlockedBuilds(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, c.path("unclustered-to-clustered", "migration", "finalizations"), nil, nil)
}

func (c *Client) transitionMigration(ctx context.Context, operation string) (*Migration, error) {
	var migration Migration
	if err := c.do(ctx, http.MethodPost, c.path("unclustered-to-clustered", "migration", operation), nil, &migration); err != nil {
		return nil, err
	}
	return &migration, nil
}
