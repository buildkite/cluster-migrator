package cli

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

func requireQueuePreparation(app *Context) (*buildkite.Migration, error) {
	migration, err := app.Client.GetMigration(app.Context)
	if err != nil {
		var apiError *buildkite.APIError
		if errors.As(err, &apiError) && apiError.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("organization migration is not configured; ask Buildkite to configure enrollment and its destination before editing queues: %w", err)
		}
		return nil, fmt.Errorf("get organization migration: %w", err)
	}
	switch migration.Phase {
	case "prepare_queues":
		return migration, nil
	case "validate_queues":
		return nil, fmt.Errorf("queue edits are frozen during validation; run cluster-migrator migration prepare-queues before editing, then cluster-migrator migration validate-queues to validate again")
	default:
		return nil, fmt.Errorf("queue edits require preparation and cannot reopen it after commitment; use cluster-migrator migration status to inspect progress and recover forward")
	}
}
