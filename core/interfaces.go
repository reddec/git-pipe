package core

import (
	"context"

	"github.com/docker/docker/client"
	"github.com/reddec/git-pipe/internal"
)

// Storage manager.
type Storage interface {
	// Restore volumes from storage. Name usually equal to daemon name.
	Restore(ctx context.Context, name string, volumeNames []string) error
	// Backup volumes to storage.
	Backup(ctx context.Context, name string, volumeNames []string) error
	// Schedule regular backup. Backup interval defined by implementation.
	Schedule(ctx context.Context, name string, volumeNames []string) *internal.Task
}

// Network manager.
type Network interface {
	// Join container to network or gather info. Should return assigned routable within network address (ip or hostname). Should not fail if container already linked.
	Join(ctx context.Context, containerID string) (address string, err error)
	// Leave network. Should not fail if container not exists or network already not connected.
	Leave(ctx context.Context, containerID string) error
	// Resolve address or address with port to routable (from application) endpoint (with port if needed).
	// Returned address may be different then after Join in case git-pipe is running as standalone application outside of docker.
	// It may work not properly in case container not running.
	Resolve(ctx context.Context, address string) (string, error)
	// ID of network in docker.
	ID() string
}

// Ingress defines routing table for incoming traffic, where domain is unique reference to service.
// Real exposed domains could be different, as well as domain could be used in a different way (ie: routing by path).
type Ingress interface {
	// Clear routing table for the group.
	Clear(ctx context.Context, group string) error
	// Set (replaces) routing table for the group.
	Set(ctx context.Context, group string, domainAddresses map[string][]string) error
}

// DNS records management.
type DNS interface {
	// Register (updated or add) domains to current IP.
	Register(ctx context.Context, domains []string) error
}

// Event emitter.
type Event interface {
	// Ready event
	Ready()
}

// Base environment for all instances.
type Base struct {
	DNS     DNS              // Register DNS name
	Ingress Ingress          // allow incoming HTTP(S) traffic to internal service
	Backup  Storage          // backup storage holder
	Network Network          // docker networking
	Docker  client.APIClient // docker api
}

// Environment context for single pipeline.
type Environment struct {
	Base
	Name      string            // unique name of package/group
	Directory string            // working directory
	Vars      map[string]string // environment variables
	Event     Event             // event emitter
}
