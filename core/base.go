package core

import (
	"context"
)

// FuncDaemon is convenient wrapper for single function as daemon.
func FuncDaemon(handler func(ctx context.Context, environment DaemonEnvironment) error) Daemon {
	return &singleRun{
		fn: handler,
	}
}

type singleRun struct {
	fn func(ctx context.Context, environment DaemonEnvironment) error
}

func (sr *singleRun) Create(_ context.Context, _ DaemonEnvironment) error {
	return nil
}

func (sr *singleRun) Run(ctx context.Context, environment DaemonEnvironment) error {
	return sr.fn(ctx, environment)
}

func (sr *singleRun) Remove(_ context.Context, _ DaemonEnvironment) error {
	return nil
}
