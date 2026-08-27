package cli

import "fmt"

type QueueStatusCmd struct {
	Queue string `arg:"" optional:"" help:"Source queue key. Omit to list every migration."`
}

func (cmd *QueueStatusCmd) Run(app *Context) error {
	if cmd.Queue == "" {
		migrations, err := app.Client.ListQueueMigrations(app.Context)
		if err != nil {
			return fmt.Errorf("list queue migrations: %w", err)
		}
		return app.Print(migrations)
	}

	migration, err := app.Client.GetQueueMigration(app.Context, cmd.Queue)
	if err != nil {
		return fmt.Errorf("get queue migration: %w", err)
	}
	return app.Print(migration)
}
