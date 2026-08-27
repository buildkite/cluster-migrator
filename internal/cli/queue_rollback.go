package cli

type QueueRollbackCmd struct {
	Queue string `arg:"" help:"Source queue key."`
}

func (cmd *QueueRollbackCmd) Run(app *Context) error {
	return setQueuePercent(app, cmd.Queue, 0, "roll back")
}
