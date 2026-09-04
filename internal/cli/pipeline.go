package cli

import (
	"context"
	"errors"
	"net/http"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

type PipelineCmd struct {
	Readiness PipelineReadinessCmd `cmd:"" help:"Assess known pipeline move blockers."`
	Move      PipelineMoveCmd      `cmd:"" help:"Permanently assign a pipeline to the cluster."`
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
	pipeline, err := client.ResolvePipelineIdentifier(ctx, identifier)
	if err != nil {
		return result, err
	}
	return run(pipeline.Slug)
}
