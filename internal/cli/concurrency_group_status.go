package cli

import "fmt"

type ConcurrencyGroupStatusCmd struct {
	Group string `arg:"" optional:"" help:"Concurrency group key. Omit to list every group."`
}

func (cmd *ConcurrencyGroupStatusCmd) Run(app *Context) error {
	if cmd.Group == "" {
		groups, err := app.Client.ListConcurrencyGroups(app.Context)
		if err != nil {
			return fmt.Errorf("list concurrency groups: %w", err)
		}
		return app.Print(groups)
	}
	group, err := app.Client.GetConcurrencyGroup(app.Context, cmd.Group)
	if err != nil {
		return fmt.Errorf("get concurrency group: %w", err)
	}
	return app.Print(group)
}
