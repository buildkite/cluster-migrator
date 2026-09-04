package buildkite

type Pipeline struct {
	ID        string `json:"id"`
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
	Pipeline                    string                              `json:"pipeline"`
	DestinationClusterID        string                              `json:"destination_cluster_id"`
	Status                      string                              `json:"status"`
	RetryAfter                  *int                                `json:"retry_after_seconds"`
	QueueObservation            PipelineQueueObservation            `json:"queue_observation"`
	ConcurrencyGroupObservation PipelineConcurrencyGroupObservation `json:"concurrency_group_observation"`
	URL                         string                              `json:"url"`
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

type PipelineQueueObservation struct {
	ObservedAt      *string                 `json:"observed_at"`
	WindowStartedAt *string                 `json:"window_started_at"`
	WindowSeconds   int                     `json:"window_seconds"`
	Complete        bool                    `json:"complete"`
	BlockingQueues  []PipelineBlockingQueue `json:"blocking_queues"`
}

type PipelineConcurrencyGroupObservation struct {
	ObservedAt                *string                    `json:"observed_at"`
	WindowStartedAt           *string                    `json:"window_started_at"`
	WindowSeconds             int                        `json:"window_seconds"`
	Complete                  bool                       `json:"complete"`
	BlockingConcurrencyGroups []BlockingConcurrencyGroup `json:"blocking_concurrency_groups"`
}

func (c *Client) pipelinePath(pipeline string, parts ...string) string {
	segments := []string{"pipelines", pipeline}
	segments = append(segments, parts...)
	return c.path(segments...)
}
