package dns

import (
	"context"
)

// DNS records management.
type DNS interface {
	// Register (updated or add) domains to current IP.
	Register(ctx context.Context, domains []string) error
}
