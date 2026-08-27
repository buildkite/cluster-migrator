package cli

type PipelineCmd struct {
	Readiness PipelineReadinessCmd `cmd:"" help:"Check queue blockers."`
	Move      PipelineMoveCmd      `cmd:"" help:"Permanently assign a ready pipeline to the cluster."`
}
