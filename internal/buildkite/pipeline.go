package buildkite

type Pipeline struct {
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	ClusterID string `json:"cluster_id"`
}

const (
	PipelineReadinessBlocked         = "blocked"
	PipelineReadinessNoKnownBlockers = "no_known_blockers"
	PipelineReadinessPending         = "observation_pending"
)

type PipelineReadiness struct {
	Pipeline                  string                     `json:"pipeline"`
	DestinationClusterID      string                     `json:"destination_cluster_id"`
	Status                    string                     `json:"status"`
	RetryAfter                *int                       `json:"retry_after_seconds"`
	BlockingQueues            []PipelineBlockingQueue    `json:"blocking_queues"`
	BlockingConcurrencyGroups []BlockingConcurrencyGroup `json:"blocking_concurrency_groups"`
	ActiveJobObservation      ActiveJobObservation       `json:"active_job_observation"`
	DependencyObservation     DependencyObservation      `json:"dependency_observation"`
	URL                       string                     `json:"url"`
}

type PipelineBlockingQueue struct {
	Queue            string   `json:"queue"`
	Reasons          []string `json:"reasons"`
	RoutedPercent    *int     `json:"routed_percent"`
	ActiveSourceJobs int      `json:"active_source_jobs"`
}

type BlockingConcurrencyGroup struct {
	Scope  string `json:"scope"`
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

type ActiveJobObservation struct {
	ObservedAt       *string `json:"observed_at"`
	ActiveLegacyJobs *int    `json:"active_legacy_jobs"`
}

type DependencyObservation struct {
	ObservedAt      *string `json:"observed_at"`
	WindowStartedAt *string `json:"window_started_at"`
	WindowSeconds   int     `json:"window_seconds"`
	Complete        bool    `json:"complete"`
}

func (c *Client) pipelinePath(pipeline string, parts ...string) string {
	segments := []string{"pipelines", pipeline}
	segments = append(segments, parts...)
	return c.path(segments...)
}
