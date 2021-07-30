package internal

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// Timer task that will be repeated till context canceled (or Stop). Error will be logged.
func Timer(ctx context.Context, interval time.Duration, runnable func(ctx context.Context) error) *Task {
	return Spawn(ctx, func(ctx context.Context) error {
		t := time.NewTicker(interval)
		defer t.Stop()

		logger := LoggerFromContext(ctx)
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				if err := runnable(ctx); err != nil {
					logger.Warn("failed task", zap.Error(err))
				}
			}
		}
	})
}

// Spawn background processing go-routine. Will be stopped when context finished or Stop() invoked.
// Stop will wait till the go-routine finish.
func Spawn(ctx context.Context, runnable func(ctx context.Context) error) *Task {
	child, cancel := context.WithCancel(ctx)
	bd := &Task{
		done:   make(chan struct{}),
		cancel: cancel,
	}

	go func() {
		defer bd.cancel()
		defer close(bd.done)
		bd.err = runnable(child)
	}()

	return bd
}

type Task struct {
	done   chan struct{}
	err    error
	cancel func()
}

// Wait for package completion.
func (bd *Task) Wait() <-chan struct{} {
	if bd == nil {
		return nil
	}
	return bd.done
}

// Stop background context and waits till the end. Returns last error.
func (bd *Task) Stop() error {
	if bd == nil {
		return nil
	}
	bd.cancel()
	<-bd.done
	return bd.err
}

// Error returned by runnable.
func (bd *Task) Error() error {
	return bd.err
}
