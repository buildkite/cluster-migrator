package cli

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

type PipelineCmd struct {
	Readiness PipelineReadinessCmd `cmd:"" help:"Assess known pipeline move blockers."`
	Move      PipelineMoveCmd      `cmd:"" help:"Permanently assign a pipeline to the cluster."`
}

func withPipelineSlugHint(err error) error {
	var apiError *buildkite.APIError
	if errors.As(err, &apiError) && apiError.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w (use the pipeline slug, not its name)", err)
	}
	return err
}
