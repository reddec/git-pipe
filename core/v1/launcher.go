package v1

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/reddec/git-pipe/core"
	"github.com/reddec/git-pipe/internal"
)

func NewLauncher(retryDeployInterval, gracefulTimeout time.Duration) *Launcher {
	return &Launcher{
		toDeploy:            make(chan core.Descriptor),
		toDestroy:           make(chan string),
		finished:            make(chan string),
		events:              make(chan core.LauncherEventMessage),
		toSubscribe:         make(chan chan core.LauncherEventMessage),
		toUnsubscribe:       make(chan (<-chan core.LauncherEventMessage)),
		retryDeployInterval: retryDeployInterval,
		gracefulTimeout:     gracefulTimeout,
		deployed:            make(map[string]func()),
		subscribers:         make(map[<-chan core.LauncherEventMessage]chan core.LauncherEventMessage),
	}
}

type Launcher struct {
	toDeploy      chan core.Descriptor
	toDestroy     chan string
	finished      chan string
	events        chan core.LauncherEventMessage
	toSubscribe   chan chan core.LauncherEventMessage
	toUnsubscribe chan (<-chan core.LauncherEventMessage)

	isRunning int32

	retryDeployInterval time.Duration
	gracefulTimeout     time.Duration
	deployed            map[string]func()
	subscribers         map[<-chan core.LauncherEventMessage]chan core.LauncherEventMessage
}

func (run *Launcher) Launch(ctx context.Context, descriptor core.Descriptor) error {
	select {
	case run.toDeploy <- descriptor:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (run *Launcher) Remove(ctx context.Context, daemon string) error {
	select {
	case run.toDestroy <- daemon:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (run *Launcher) Run(ctx context.Context, environment core.Environment) {
	if !atomic.CompareAndSwapInt32(&run.isRunning, 0, 1) {
		// fool proof
		return
	}
	defer atomic.StoreInt32(&run.isRunning, 0)
	logger := internal.SubLogger(ctx, "launcher")

LOOP:
	for {
		select {
		case <-ctx.Done():
			break LOOP
		case descriptor := <-run.toDeploy:
			run.spawn(ctx, descriptor, environment)
		case name := <-run.toDestroy:
			run.destroy(name)
		case name := <-run.finished:
			delete(run.deployed, name)
		case event := <-run.events:
			run.distributeEvent(logger, event)
		case ch := <-run.toSubscribe:
			run.subscribers[ch] = ch
		case ch := <-run.toUnsubscribe:
			if v, ok := run.subscribers[ch]; ok {
				close(v)
				delete(run.subscribers, ch)
			}
		}
	}

	// request all daemons to finish
	for _, cancel := range run.deployed {
		cancel()
	}

	// minimal processing: only un-subscribe, events and finish
	for len(run.deployed) > 0 {
		select {
		case name := <-run.finished:
			delete(run.deployed, name)
		case event := <-run.events:
			run.distributeEvent(logger, event)
		case ch := <-run.toUnsubscribe:
			if v, ok := run.subscribers[ch]; ok {
				close(v)
				delete(run.subscribers, ch)
			}
		}
	}

	// cleanup events listeners
	for _, ch := range run.subscribers {
		close(ch)
	}
}

func (run *Launcher) Subscribe(ctx context.Context, buffer int) (<-chan core.LauncherEventMessage, error) {
	ch := make(chan core.LauncherEventMessage, buffer)
	select {
	case run.toSubscribe <- ch:
	case <-ctx.Done():
		close(ch)
		return nil, ctx.Err()
	}
	return ch, nil
}

func (run *Launcher) Unsubscribe(ctx context.Context, ch <-chan core.LauncherEventMessage) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case run.toUnsubscribe <- ch:
		return nil
	}
}

func (run *Launcher) distributeEvent(logger internal.Logger, event core.LauncherEventMessage) {
	for _, ch := range run.subscribers {
		select {
		case ch <- event:
		default:
			logger.Println("notification channel overflow")
		}
	}
}

func (run *Launcher) notify(event core.LauncherEvent, descriptor core.Descriptor, err error) {
	run.events <- core.LauncherEventMessage{
		Event:  event,
		Daemon: descriptor.Name,
		Error:  err,
	}
}

func (run *Launcher) destroy(name string) {
	if cancel, ok := run.deployed[name]; ok {
		cancel()
	}
}

func (run *Launcher) spawn(ctx context.Context, descriptor core.Descriptor, environment core.Environment) {
	if run.isDeployed(descriptor.Name) {
		return
	}

	child, cancel := context.WithCancel(ctx)
	go func() {
		run.runDaemonLoop(child, descriptor, environment)
		run.finished <- descriptor.Name
	}()
	run.deployed[descriptor.Name] = cancel
}

func (run *Launcher) isDeployed(name string) bool {
	_, ok := run.deployed[name]
	return ok
}

func (run *Launcher) runDaemonLoop(global context.Context, descriptor core.Descriptor, environment core.Environment) {
	ctx := internal.WithSubLogger(global, descriptor.Name)
	for {
		err := run.runDaemon(ctx, descriptor, environment)
		if errors.Is(err, context.Canceled) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(run.retryDeployInterval):
		}
	}
}

func (run *Launcher) runDaemon(ctx context.Context, descriptor core.Descriptor, globalEnvironment core.Environment) error {
	environment := core.DaemonEnvironment{
		Name:        descriptor.Name,
		Environment: globalEnvironment,
	}
	if err := descriptor.Daemon.Create(ctx, environment); err != nil {
		run.notify(core.LauncherEventCreateFailed, descriptor, nil)
		return fmt.Errorf("build: %w", err)
	}
	run.notify(core.LauncherEventCreated, descriptor, nil)

	if err := descriptor.Daemon.Start(ctx, environment); err != nil {
		run.notify(core.LauncherEventStartFailed, descriptor, nil)
		return fmt.Errorf("start: %w", err)
	}
	run.notify(core.LauncherEventStarted, descriptor, nil)

	<-ctx.Done()
	graceCtx, graceCancel := context.WithTimeout(context.Background(), run.gracefulTimeout)
	defer graceCancel()

	var group *multierror.Error

	group = multierror.Append(group, ctx.Err())

	if err := descriptor.Daemon.Stop(graceCtx, environment); err != nil {
		run.notify(core.LauncherEventStopFailed, descriptor, nil)
		group = multierror.Append(group, err)
	}
	run.notify(core.LauncherEventStopped, descriptor, nil)

	removeCtx, removeCancel := context.WithTimeout(context.Background(), run.gracefulTimeout)
	defer removeCancel()

	if err := descriptor.Daemon.Remove(removeCtx, environment); err != nil {
		run.notify(core.LauncherEventRemoveFailed, descriptor, nil)
		group = multierror.Append(group, err)
	}
	run.notify(core.LauncherEventRemoved, descriptor, nil)

	return group
}
