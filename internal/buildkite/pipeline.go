package buildkite

type Pipeline struct {
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	ClusterID string `json:"cluster_id"`
}

type PipelineReadiness struct {
	Pipeline             string   `json:"pipeline"`
	DestinationClusterID string   `json:"destination_cluster_id"`
	Ready                bool     `json:"ready"`
	BlockingQueues       []string `json:"blocking_queues"`
	URL                  string   `json:"url"`
}

func (c *Client) pipelinePath(pipeline string, parts ...string) string {
	segments := []string{"pipelines", pipeline}
	segments = append(segments, parts...)
	return c.path(segments...)
}
