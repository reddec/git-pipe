package core

type LauncherEvent int

const (
	LauncherEventCreated LauncherEvent = iota
	LauncherEventCreateFailed
	LauncherEventStarted
	LauncherEventStartFailed
	LauncherEventStopped
	LauncherEventStopFailed
	LauncherEventRemoved
	LauncherEventRemoveFailed
)

type LauncherEventMessage struct {
	Event  LauncherEvent
	Daemon string
	Error  error // nil for non-Failed events
}

func WaitForLauncherEvent(events <-chan LauncherEventMessage, daemon string, filter LauncherEvent) bool {
	for item := range events {
		if item.Event == filter && item.Daemon == daemon {
			return true
		}
	}
	return false
}

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
