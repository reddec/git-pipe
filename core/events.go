package core

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
	for item := range events {
		if (item.Event&mask) != 0 && item.Daemon == daemon {
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
