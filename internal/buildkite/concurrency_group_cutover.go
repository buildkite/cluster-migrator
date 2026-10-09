package buildkite

import (
	"context"
	"net/http"
)

func (c *Client) StartConcurrencyGroupCutover(ctx context.Context, scope, key, clusterID string) (*ConcurrencyGroup, error) {
	request := struct {
		DestinationClusterID string `json:"destination_cluster_id"`
	}{DestinationClusterID: clusterID}

	var result ConcurrencyGroup
	if err := c.do(ctx, http.MethodPost, c.concurrencyGroupPath(scope, key, "cutover"), request, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
