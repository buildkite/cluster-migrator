package buildkite

type ConcurrencyGroup struct {
	Key                  string   `json:"key"`
	State                string   `json:"state"`
	DestinationClusterID string   `json:"destination_cluster_id"`
	BlockingQueues       []string `json:"blocking_queues"`
	RunningSourceJobs    int      `json:"running_source_jobs"`
	WaitingJobs          int      `json:"waiting_jobs"`
	URL                  string   `json:"url"`
}

func (c *Client) concurrencyGroupPath(group string, parts ...string) string {
	segments := []string{"cluster-migration", "concurrency-groups", group}
	segments = append(segments, parts...)
	return c.path(segments...)
}
