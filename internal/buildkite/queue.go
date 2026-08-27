package buildkite

import "time"

type QueueMigration struct {
	QueueKey      string           `json:"queue_key"`
	Destination   QueueDestination `json:"destination"`
	RoutedPercent int              `json:"routed_percent"`
	URL           string           `json:"url"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

type QueueDestination struct {
	ClusterID string `json:"cluster_id"`
	QueueID   string `json:"queue_id"`
	QueueKey  string `json:"queue_key"`
}

type ClusterQueue struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

func (c *Client) queuePath(queue string) string {
	return c.path("cluster-queue-migrations", queue)
}
