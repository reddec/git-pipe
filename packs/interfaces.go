package packs

import "context"

const (
	Namespace = "git-pipe-app" // prefix for names of volumes and applications
)

// Service exposed by package.
type Service struct {
	Group     string   // package name / group name. Should be unique within one instance of git-pipe.
	Name      string   // service name. Should be unique within one group.
	Domain    string   // suggested domain name.
	Addresses []string // IP:PORT addresses of service (could be several in case of scaling)
}

// Pack defines repo-specific package implementation.
type Pack interface {
	// Volumes defined in repo and which should be added to backup.
	Volumes(ctx context.Context) ([]string, error)
	// Build package.
	Build(ctx context.Context, env map[string]string) error
	// Start package (also should stop previous instances if possible) and returns exposed services.
	Start(ctx context.Context, env map[string]string) ([]Service, error)
	// Stop package. Will be executed only after successful Start.
	Stop(ctx context.Context) error
	// String representation of package type.
	String() string
}

// Network definition in docker.
type Network struct {
	ID   string // docker network hash
	Name string // docker network human readable name
}
