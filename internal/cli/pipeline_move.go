package cli

import (
	"fmt"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

type PipelineMoveCmd struct {
	Pipeline string `arg:"" help:"Pipeline ID, name, or slug."`
}

func (cmd *PipelineMoveCmd) Run(app *Context) error {
	pipeline, err := runWithPipelineIdentifierFallback(app.Context, app.Client, cmd.Pipeline, func(identifier string) (*buildkite.Pipeline, error) {
		return app.Client.MovePipeline(app.Context, identifier)
	})
	if err != nil {
		return fmt.Errorf("move pipeline: %w", err)
	}
	return app.Print(PipelineMoveResult{Pipeline: pipeline, Destination: pipeline.ClusterID})
}
