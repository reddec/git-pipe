package core

import "context"

// Service exposed by someone.
type Service struct {
	Namespace string   // daemon name / package name / group name.
	Name      string   // service name. Should be unique within one namespace.
	Addresses []string // IP:PORT addresses of service (could be several in case of scaling)
	Domain    string   // optional, if not defined - domain will be computed from name and namespace.
}

func (srv *Service) Label() string {
	return srv.Name + "@" + srv.Namespace
}

// Registry of services.
type Registry interface {
	// Register service.
	Register(srv Service) error
	// Unregister service by name.
	Unregister(namespace, name string)
	// Find service by name.
	Find(namespace, name string) (Service, error)
	// Lookup service by full domain name.
	Lookup(domain string) (Service, error)
	// All registered services.
	All() []Service
	// Subscribe to all events from registry. Unsubscribe MUST be called to free resources.
	// Replay flag means to stream events about all already subscribed services.
	Subscribe(buffer int, replay bool) RegistryEventStream
	// Unsubscribe from events. It closes channel.
	Unsubscribe(ch RegistryEventStream)
}

// Descriptor of daemon to launch.
type Descriptor struct {
	Name   string // unique name.
	Daemon Daemon // definition
}

type Launcher interface {
	// Launch daemon in background.
	// Lifecycle is:
	//   - Create
	//   - Start (restart loop)
	//   - Stop
	//   - Remove
	// Multiple daemons with the same name will be ignored (only first will be processed).
	Launch(ctx context.Context, descriptor Descriptor) error
	// Remove daemon in background. Also will call Stop and Remove.
	Remove(ctx context.Context, daemon string) error
	// Subscribe for events. Unsubscribe MUST be called to free resources.
	Subscribe(ctx context.Context, buffer int) (<-chan LauncherEventMessage, error)
	// Unsubscribe from events. It also closes channel.
	Unsubscribe(ctx context.Context, ch <-chan LauncherEventMessage) error
}

// Daemon description. It's guaranteed that each function will be called in a single go-routine.
type Daemon interface {
	// Create required resources.
	Create(ctx context.Context, environment DaemonEnvironment) error
	// Start resource. Should not block.
	Start(ctx context.Context, environment DaemonEnvironment) error
	// Stop resource. Should block till everything stopped.
	Stop(ctx context.Context, environment DaemonEnvironment) error
	// Remove temporary resources if needed.
	Remove(ctx context.Context, environment DaemonEnvironment) error
}

type DaemonEnvironment struct {
	Name        string      // daemon name
	Environment Environment // global environment
}

type Environment interface {
	Launcher() Launcher
	Registry() Registry
}
