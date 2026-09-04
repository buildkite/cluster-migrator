package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

const pipelineReadinessTimeout = time.Minute

type PipelineReadinessCmd struct {
	Pipeline           string `arg:"" name:"pipeline-slug" help:"Pipeline slug (not pipeline name)."`
	DestinationCluster string `name:"destination-cluster" required:"" help:"Destination cluster name or ID."`
}

func (cmd *PipelineReadinessCmd) Run(app *Context) error {
	cluster, err := app.Client.ResolveCluster(app.Context, cmd.DestinationCluster)
	if err != nil {
		return fmt.Errorf("resolve destination cluster: %w", err)
	}
	readiness, err := getPipelineReadiness(app, cmd.Pipeline, cluster.ID)
	if err != nil {
		return err
	}
	if err := app.Print(readiness); err != nil {
		return err
	}
	if readiness.Status == buildkite.PipelineReadinessBlocked {
		return fmt.Errorf("pipeline has known blockers")
	}
	if readiness.Status != buildkite.PipelineReadinessNoKnownBlockers {
		return fmt.Errorf("unknown pipeline assessment status %q", readiness.Status)
	}
	return nil
}

func getPipelineReadiness(app *Context, pipeline, clusterID string) (*buildkite.PipelineReadiness, error) {
	ctx, cancel := context.WithTimeout(app.Context, pipelineReadinessTimeout)
	defer cancel()
	retries := 0
	retryMessagePrinted := false

	for {
		readiness, err := app.Client.GetPipelineReadiness(ctx, pipeline, clusterID)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, pipelineReadinessDeadlineError(app.Context)
			}
			return nil, fmt.Errorf("get pipeline readiness: %w", withPipelineSlugHint(err))
		}
		if readiness.Status != buildkite.PipelineReadinessPending {
			return readiness, nil
		}
		if readiness.RetryAfter == nil || *readiness.RetryAfter <= 0 {
			retryAfter := 0
			if readiness.RetryAfter != nil {
				retryAfter = *readiness.RetryAfter
			}
			return nil, fmt.Errorf("invalid retry_after_seconds: %d", retryAfter)
		}

		retryAfter := *readiness.RetryAfter
		if retries > 0 && !retryMessagePrinted {
			if _, err := fmt.Fprintf(app.ErrorOutput, "Pipeline assessment is still being prepared; retrying every %d %s…\n", retryAfter, pluralize(retryAfter, "second")); err != nil {
				return nil, err
			}
			retryMessagePrinted = true
		}
		retries++
		if err := app.wait(ctx, time.Duration(retryAfter)*time.Second); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, pipelineReadinessDeadlineError(app.Context)
			}
			return nil, fmt.Errorf("wait to retry pipeline assessment: %w", err)
		}
	}
}

func pipelineReadinessDeadlineError(parent context.Context) error {
	if err := parent.Err(); err != nil {
		return err
	}
	return fmt.Errorf("pipeline assessment did not become available within %s", pipelineReadinessTimeout)
}
