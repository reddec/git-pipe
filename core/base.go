package core

import (
	"context"
)

// FuncDaemon is convenient wrapper for single function as daemon.
func FuncDaemon(handler func(ctx context.Context, environment DaemonEnvironment)) Daemon {
	return &singleRun{
		fn: handler,
	}
}

type singleRun struct {
	cancel func()
	done   chan struct{}
	fn     func(ctx context.Context, environment DaemonEnvironment)
}

func (sr *singleRun) Create(_ context.Context, _ DaemonEnvironment) error {
	return nil
}

func (sr *singleRun) Start(_ context.Context, environment DaemonEnvironment) error {
	ch, cancel := context.WithCancel(context.Background())
	sr.cancel = cancel
	sr.done = make(chan struct{})
	go func() {
		defer close(sr.done)
		sr.fn(ch, environment)
	}()

	return nil
}

func (sr *singleRun) Stop(ctx context.Context, _ DaemonEnvironment) error {
	sr.cancel()
	select {
	case <-sr.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (sr *singleRun) Remove(_ context.Context, _ DaemonEnvironment) error {
	return nil
}
