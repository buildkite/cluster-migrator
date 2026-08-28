package cli

import "fmt"

type QueueSetPercentCmd struct {
	Queue string `arg:"" help:"Source queue key."`
	To    int    `name:"to" required:"" help:"Absolute routed percentage."`
}

func (cmd *QueueSetPercentCmd) Run(app *Context) error {
	return setQueuePercent(app, cmd.Queue, cmd.To, "set routed percentage")
}

func setQueuePercent(app *Context, queue string, percent int, action string) error {
	if percent < 0 || percent > 100 {
		return fmt.Errorf("--to must be between 0 and 100")
	}

	current, err := app.Client.GetQueueMigration(app.Context, queue)
	if err != nil {
		return fmt.Errorf("get queue migration: %w", err)
	}
	change := Change{
		Action:      action,
		Resource:    "queue " + queue,
		FromPercent: &current.RoutedPercent,
		ToPercent:   &percent,
		DryRun:      app.DryRun,
	}
	if current.RoutedPercent == percent || app.DryRun {
		return app.Print(change)
	}
	if err := app.Confirm(change); err != nil {
		return err
	}

	updated, err := app.Client.SetQueueMigrationPercent(app.Context, queue, percent)
	if err != nil {
		return fmt.Errorf("set routed percentage: %w", err)
	}
	return app.Print(updated)
}
