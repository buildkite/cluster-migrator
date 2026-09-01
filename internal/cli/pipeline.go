package cli

type PipelineCmd struct {
	Readiness PipelineReadinessCmd `cmd:"" help:"Assess known pipeline move blockers."`
	Move      PipelineMoveCmd      `cmd:"" help:"Permanently assign a pipeline to the cluster."`
}
