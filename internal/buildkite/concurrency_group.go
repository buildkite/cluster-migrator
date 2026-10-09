package buildkite

type ConcurrencyGroup struct {
	Scope                string   `json:"scope"`
	Key                  string   `json:"key"`
	State                string   `json:"state"`
	DestinationClusterID string   `json:"destination_cluster_id"`
	BlockingQueues       []string `json:"blocking_queues"`
	RunningSourceJobs    int      `json:"running_source_jobs"`
	WaitingJobs          int      `json:"waiting_jobs"`
	URL                  string   `json:"url"`
}

// A concurrency group is identified by its scope and key; equal keys in different scopes are distinct groups.
func (c *Client) concurrencyGroupPath(scope, key string, parts ...string) string {
	segments := []string{"cluster-queue-migrations", "concurrency-groups", scope, key}
	segments = append(segments, parts...)
	return c.path(segments...)
}
