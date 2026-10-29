package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

type PipelineCmd struct {
	Readiness PipelineReadinessCmd `cmd:"" help:"Assess known pipeline move blockers for the organization's fixed migration destination."`
	Move      PipelineMoveCmd      `cmd:"" help:"Permanently assign a pipeline to the organization's fixed migration destination."`
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
	if strings.Contains(apiError.Message, "migration is not configured") {
		return result, fmt.Errorf("%w; run cluster-migrator migration configure --destination-cluster <cluster>", err)
	}
	slug, resolveErr := client.ResolvePipelineSlug(ctx, identifier)
	if resolveErr != nil {
		return result, fmt.Errorf("%w; resolve pipeline identifier: %v", err, resolveErr)
	}
	result, err = run(slug)
	if errors.As(err, &apiError) && apiError.StatusCode == http.StatusNotFound && strings.Contains(apiError.Message, "migration is not configured") {
		return result, fmt.Errorf("%w; run cluster-migrator migration configure --destination-cluster <cluster>", err)
	}
	return result, err
}
