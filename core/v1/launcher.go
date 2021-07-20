package v1

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/reddec/git-pipe/core"
	"github.com/reddec/git-pipe/internal"
)

func NewLauncher(retryDeployInterval, cleanupTimeout time.Duration) *Launcher {
	return &Launcher{
		toDeploy:            make(chan core.Descriptor),
		toDestroy:           make(chan string),
		finished:            make(chan string),
		events:              make(chan core.LauncherEventMessage),
		toSubscribe:         make(chan subscription),
		toUnsubscribe:       make(chan (<-chan core.LauncherEventMessage)),
		retryDeployInterval: retryDeployInterval,
		cleanupTimeout:      cleanupTimeout,
		deployed:            make(map[string]*runningDaemon),
		subscribers:         make(map[<-chan core.LauncherEventMessage]chan core.LauncherEventMessage),
	}
}

type subscription struct {
	ch     chan core.LauncherEventMessage
	replay bool
}

type Launcher struct {
	toDeploy      chan core.Descriptor
	toDestroy     chan string
	finished      chan string
	events        chan core.LauncherEventMessage
	toSubscribe   chan subscription
	toUnsubscribe chan (<-chan core.LauncherEventMessage)

	isRunning int32

	retryDeployInterval time.Duration
	cleanupTimeout      time.Duration
	deployed            map[string]*runningDaemon
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
		case subs := <-run.toSubscribe:
			run.subscribers[subs.ch] = subs.ch
			if subs.replay {
				run.replay(subs.ch)
			}
		case ch := <-run.toUnsubscribe:
			if v, ok := run.subscribers[ch]; ok {
				close(v)
				delete(run.subscribers, ch)
			}
		}
	}

	// request all daemons to finish
	for _, d := range run.deployed {
		d.stop()
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

func (run *Launcher) Subscribe(ctx context.Context, buffer int, replay bool) (<-chan core.LauncherEventMessage, error) {
	ch := make(chan core.LauncherEventMessage, buffer)
	select {
	case run.toSubscribe <- subscription{ch: ch, replay: replay}:
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

func (run *Launcher) replay(to chan core.LauncherEventMessage) {
	for _, d := range run.deployed {
		select {
		case to <- d.lastEvent:
		default:
			return
		}
	}
}

func (run *Launcher) distributeEvent(logger internal.Logger, event core.LauncherEventMessage) {
	if d, ok := run.deployed[event.Daemon]; ok {
		d.lastEvent = event
	}
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
	if d, ok := run.deployed[name]; ok {
		d.stop()
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
	run.deployed[descriptor.Name] = &runningDaemon{
		stop: cancel,
		lastEvent: core.LauncherEventMessage{
			Event:  core.LauncherEventScheduled,
			Daemon: descriptor.Name,
		},
	}
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
	environment := &daemonEnvironment{
		descriptor: descriptor,
		global:     globalEnvironment,
		readyFn: func() {
			run.notify(core.LauncherEventReady, descriptor, nil)
		},
	}
	if err := descriptor.Daemon.Create(ctx, environment); err != nil {
		run.notify(core.LauncherEventCreateFailed, descriptor, nil)
		return fmt.Errorf("build: %w", err)
	}
	run.notify(core.LauncherEventCreated, descriptor, nil)

	if err := descriptor.Daemon.Run(ctx, environment); err != nil {
		run.notify(core.LauncherEventRunFailed, descriptor, nil)
		// fallthrough to remove allocated resources
	}

	run.notify(core.LauncherEventStopped, descriptor, nil)
	removeCtx, removeCancel := context.WithTimeout(context.Background(), run.cleanupTimeout)
	defer removeCancel()

	if err := descriptor.Daemon.Remove(removeCtx, environment); err != nil {
		run.notify(core.LauncherEventRemoveFailed, descriptor, nil)
		return fmt.Errorf("remove: %w", err)
	}
	run.notify(core.LauncherEventRemoved, descriptor, nil)

	return nil
}

type runningDaemon struct {
	stop      func()
	lastEvent core.LauncherEventMessage
}

type daemonEnvironment struct {
	descriptor core.Descriptor
	global     core.Environment
	ready      int32
	readyFn    func()
}

func (d *daemonEnvironment) Name() string {
	return d.descriptor.Name
}

func (d *daemonEnvironment) Global() core.Environment {
	return d.global
}

func (d *daemonEnvironment) Ready() {
	if atomic.CompareAndSwapInt32(&d.ready, 0, 1) {
		d.readyFn()
	}
}
