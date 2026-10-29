package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

type PipelineCmd struct {
	Readiness PipelineReadinessCmd `cmd:"" help:"Assess known pipeline move blockers for the organization's fixed migration destination."`
	Move      PipelineMoveCmd      `cmd:"" help:"Permanently assign a pipeline to the organization's fixed migration destination."`
}

func resolvePipelineDestination(app *Context) (*buildkite.Cluster, error) {
	migration, err := app.Client.GetMigration(app.Context)
	if err != nil {
		var apiError *buildkite.APIError
		if errors.As(err, &apiError) && apiError.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("organization migration is not configured; run cluster-migrator migration configure --destination-cluster <cluster>: %w", err)
		}
		return nil, fmt.Errorf("get organization migration: %w; check cluster-migrator migration status and API token access", err)
	}
	if migration.DestinationClusterID == "" {
		return nil, fmt.Errorf("organization migration has no destination_cluster_id; check cluster-migrator migration status and contact Buildkite")
	}
	cluster, err := app.Client.ResolveCluster(app.Context, migration.DestinationClusterID)
	if err != nil {
		return nil, fmt.Errorf("resolve organization migration destination %q: %w; check cluster-migrator migration status and API token access to the destination cluster", migration.DestinationClusterID, err)
	}
	return cluster, nil
}

func runWithPipelineIdentifierFallback[T any](
	ctx context.Context,
	client *buildkite.Client,
	identifier string,
	run func(string) (T, error),
) (T, error) {
	result, err := run(identifier)
	if err == nil {
		return result, nil
	}
	var apiError *buildkite.APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusNotFound {
		return result, err
	}
	slug, err := client.ResolvePipelineSlug(ctx, identifier)
	if err != nil {
		return result, err
	}
	return run(slug)
}
