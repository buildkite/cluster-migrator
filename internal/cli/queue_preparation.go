package cli

import (
	"fmt"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

func requireQueuePreparation(app *Context) (*buildkite.Migration, error) {
	migration, err := app.Client.GetMigration(app.Context)
	if err != nil {
		return nil, fmt.Errorf("get organization migration: %w", err)
	}
	app.rememberMigration(migration)
	switch migration.Phase {
	case "prepare_queues":
		return migration, nil
	case "validate_queues":
		return nil, fmt.Errorf("queue edits are frozen in phase %s. Allowed phase: prepare_queues; run cluster-migrator migration set-phase prepare-queues before editing, then cluster-migrator migration set-phase validate-queues to validate again", migration.Phase)
	default:
		return nil, fmt.Errorf("queue edits cannot run in phase %s. Allowed phase: prepare_queues. Preparation cannot reopen after commitment; use cluster-migrator migration status to inspect progress and recover forward", migration.Phase)
	}
}
