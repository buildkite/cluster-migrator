package buildkite

import (
	"context"
	"fmt"
	"net/http"
)

func (c *Client) RequireClusterQueue(ctx context.Context, clusterID, queueKey string) error {
	var queues []ClusterQueue
	path := c.path("clusters", clusterID, "queues") + "?per_page=100"
	if err := c.do(ctx, http.MethodGet, path, nil, &queues); err != nil {
		return err
	}
	for _, queue := range queues {
		if queue.Key == queueKey {
			return nil
		}
	}
	return fmt.Errorf("cluster %q has no queue with key %q", clusterID, queueKey)
}

func (c *Client) ConfigureQueueMigration(ctx context.Context, queue, clusterID string) (*QueueMigration, error) {
	request := struct {
		ClusterID string `json:"cluster_id"`
		QueueKey  string `json:"queue_key"`
	}{ClusterID: clusterID, QueueKey: queue}

	var migration QueueMigration
	if err := c.do(ctx, http.MethodPost, c.path("cluster-queue-migrations"), request, &migration); err != nil {
		return nil, err
	}
	return &migration, nil
}
