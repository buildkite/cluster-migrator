package cli

type QueueCmd struct {
	Configure  QueueConfigureCmd  `cmd:"" help:"Map a source queue to an existing cluster queue at 0%."`
	SetPercent QueueSetPercentCmd `cmd:"" name:"set-percent" help:"Set the absolute percentage of new jobs routed to the cluster queue."`
	Rollback   QueueRollbackCmd   `cmd:"" help:"Return new jobs to the unclustered queue."`
	Status     QueueStatusCmd     `cmd:"" help:"Show one or all queue migrations."`
	Metrics    QueueMetricsCmd    `cmd:"" help:"Show recent destination queue activity."`
}
