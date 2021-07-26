package core

import (
	"context"

	"go.uber.org/zap"
)

//go:generate stringer -type LauncherEvent -trimprefix LauncherEvent
type LauncherEvent int

const (
	LauncherEventScheduled    LauncherEvent = 0b00000001
	LauncherEventCreated      LauncherEvent = 0b00000010
	LauncherEventCreateFailed LauncherEvent = 0b00000100
	LauncherEventReady        LauncherEvent = 0b00001000
	LauncherEventRunFailed    LauncherEvent = 0b00010000
	LauncherEventStopped      LauncherEvent = 0b00100000
	LauncherEventRemoved      LauncherEvent = 0b01000000
	LauncherEventRemoveFailed LauncherEvent = 0b10000000
)

type LauncherEventMessage struct {
	Event  LauncherEvent
	Daemon string
	Error  error // nil for non-Failed events
}

func WaitForLauncherEvent(events <-chan LauncherEventMessage, daemon string, mask LauncherEvent) bool {
	return WaitForLauncherEventContext(context.Background(), events, daemon, mask)
}

func WaitForLauncherEventContext(ctx context.Context, events <-chan LauncherEventMessage, daemon string, mask LauncherEvent) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		case item, ok := <-events:
			if !ok {
				return false
			}
			if (item.Event&mask) != 0 && item.Daemon == daemon {
				return true
			}
		}
	}
}

func WaitForEvent(ctx context.Context, launcher Launcher, daemon string, mask LauncherEvent) bool {
	subs, err := launcher.Subscribe(ctx, 1024, true)
	if err != nil {
		return false
	}
	defer launcher.Unsubscribe(ctx, subs)

	return WaitForLauncherEventContext(ctx, subs, daemon, mask)
}

func LogEvents(logger *zap.Logger, events <-chan LauncherEventMessage) {
	go func() {
		for event := range events {
			logger.Info("new event", zap.String("daemon", event.Daemon), zap.Error(event.Error), zap.String("event", event.Event.String()))
		}
	}()
}

//go:generate stringer -type RegistryEvent -trimprefix RegistryEvent
type RegistryEvent int

const (
	RegistryEventRegistered RegistryEvent = iota
	RegistryEventUnregistered
)

type RegistryEventMessage struct {
	Event   RegistryEvent
	Service Service
}

type RegistryEventStream <-chan RegistryEventMessage
