package buildkite

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
)

type OrganizationMigration struct {
	ID                   string             `json:"id"`
	OrganizationID       string             `json:"organization_id"`
	DestinationClusterID string             `json:"destination_cluster_id"`
	Phase                string             `json:"phase"`
	CreatedByID          string             `json:"created_by_id"`
	URL                  string             `json:"url"`
	CreatedAt            string             `json:"created_at"`
	UpdatedAt            string             `json:"updated_at"`
	PhaseVersion         *string            `json:"phase_version,omitempty"`
	LatestTransition     *MigrationEvent    `json:"latest_transition"`
	Progress             *MigrationProgress `json:"progress,omitempty"`
}

type MigrationEvent struct {
	EventID    string  `json:"event_id"`
	From       string  `json:"from"`
	To         string  `json:"to"`
	Source     string  `json:"source"`
	ActorID    *string `json:"actor_id"`
	OccurredAt string  `json:"occurred_at"`
}

type MigrationProgress struct {
	ConcurrencyGroups *ConcurrencyGroupProgress `json:"concurrency_groups"`
	Pipelines         *PipelineProgress         `json:"pipelines"`
}

type MigrationProgressSummary struct {
	Authorization *MigrationAuthorization `json:"authorization"`
	Observation   *MigrationObservation   `json:"observation"`
	Sample        *MigrationSample        `json:"sample"`
}

type MigrationAuthorization struct {
	EventID      string  `json:"event_id"`
	AuthorizedAt string  `json:"authorized_at"`
	ActorID      *string `json:"actor_id"`
}

type MigrationObservation struct {
	Status     string  `json:"status"`
	ObservedAt *string `json:"observed_at"`
	ExpiresAt  *string `json:"expires_at"`
	Cached     bool    `json:"cached"`
}

type MigrationSample struct {
	Limit   int            `json:"limit"`
	Scanned int            `json:"scanned"`
	HasMore bool           `json:"has_more"`
	Counts  map[string]int `json:"counts"`
}

type ConcurrencyGroupProgress struct {
	MigrationProgressSummary
	Items []ConcurrencyGroupProgressItem `json:"items"`
}

type ConcurrencyGroupProgressItem struct {
	Scope    string             `json:"scope"`
	Key      string             `json:"key"`
	State    string             `json:"state"`
	Held     bool               `json:"held"`
	Blockers []MigrationBlocker `json:"blockers"`
}

type PipelineProgress struct {
	MigrationProgressSummary
	Items []PipelineProgressItem `json:"items"`
}

type PipelineProgressItem struct {
	PipelineID string             `json:"pipeline_id"`
	Slug       string             `json:"slug"`
	Archived   bool               `json:"archived"`
	State      string             `json:"state"`
	Blockers   []MigrationBlocker `json:"blockers"`
}

type MigrationAssessment struct {
	Operation      string               `json:"operation"`
	MigrationID    *string              `json:"migration_id"`
	OrganizationID string               `json:"organization_id"`
	Phase          *string              `json:"phase"`
	PhaseVersion   *string              `json:"phase_version"`
	Transition     *MigrationTransition `json:"transition"`
	Ready          bool                 `json:"ready"`
	Status         string               `json:"status"`
	Blockers       []MigrationBlocker   `json:"blockers"`
	ObservedAt     string               `json:"observed_at"`
	ExpiresAt      *string              `json:"expires_at"`
	Cached         bool                 `json:"cached"`
}

type MigrationTransition struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type MigrationBlocker struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type MigrationOperationRequest struct {
	ExpectedPhaseVersion    string `json:"expected_phase_version"`
	AcknowledgeIrreversible bool   `json:"acknowledge_irreversible,omitempty"`
	FinalizeBlockedBuilds   bool   `json:"finalize_blocked_builds,omitempty"`
}

type MigrationOperationResult struct {
	Outcome   string                 `json:"outcome"`
	Migration *OrganizationMigration `json:"migration,omitempty"`
	Readiness *MigrationAssessment   `json:"readiness"`
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

func (c *Client) GetMigrationReadiness(ctx context.Context, operation, destinationClusterID string, finalizeBlockedBuilds bool) (*MigrationAssessment, error) {
	query := url.Values{"operation": {operation}}
	if destinationClusterID != "" {
		query.Set("destination_cluster_id", destinationClusterID)
	}
	if finalizeBlockedBuilds {
		query.Set("finalize_blocked_builds", "true")
	}
	var assessment MigrationAssessment
	if err := c.do(ctx, http.MethodGet, c.path("unclustered-to-clustered-migration", "readiness")+"?"+query.Encode(), nil, &assessment); err != nil {
		return nil, err
	}
	return &assessment, nil
}

func (c *Client) RunMigrationOperation(ctx context.Context, operation string, request MigrationOperationRequest) (*MigrationOperationResult, error) {
	var result MigrationOperationResult
	err := c.do(ctx, http.MethodPost, c.path("unclustered-to-clustered-migration", operation), request, &result)
	if err != nil {
		var apiError *APIError
		if errors.As(err, &apiError) && apiError.StatusCode == http.StatusConflict {
			if decodeErr := json.Unmarshal(apiError.Body, &result); decodeErr == nil && result.Outcome == "blocked" && result.Readiness != nil {
				return &result, err
			}
		}
		return nil, err
	}
	return &result, nil
}
