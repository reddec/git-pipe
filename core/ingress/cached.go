package ingress

import (
	"context"
	"errors"
	"sync"

	"github.com/reddec/git-pipe/core"
	"github.com/reddec/git-pipe/internal"
	"go.uber.org/zap"
)

type Record struct {
	Domain    string   // unique reference to service.
	Addresses []string // host:port, could be multiple in case of scale factor > 1
	Group     string   // namespace, not used in routing
}

type Backend interface {
	// Set new state of routing tables. All records must have unique domains.
	Set(ctx context.Context, records []Record) error
}

type BackendFunc func(ctx context.Context, records []Record) error

func (bf BackendFunc) Set(ctx context.Context, records []Record) error {
	return bf(ctx, records)
}

// New cached ingress. Cache layer simplifies implementation of ingress.
//
// It's guaranteed:
// - Backend.Set invoked strictly synchronized
// - All records have unique domains
func New(backend Backend) core.Ingress {
	return &cachedIngress{
		backend:           backend,
		domainsByGroup:    map[string][]string{},
		addressesByDomain: map[string]address{},
	}
}

type cachedIngress struct {
	lock              sync.Mutex
	backend           Backend
	domainsByGroup    map[string][]string
	addressesByDomain map[string]address // domain -> []addresses
}

func (ci *cachedIngress) Clear(ctx context.Context, group string) error {
	ci.lock.Lock()
	defer ci.lock.Unlock()
	ci.clearGroup(group)
	return ci.updateState(ctx)
}

func (ci *cachedIngress) Set(ctx context.Context, group string, domainAddresses map[string][]string) error {
	ci.lock.Lock()
	defer ci.lock.Unlock()
	for domain := range domainAddresses {
		if addr, exist := ci.addressesByDomain[domain]; exist && group != addr.group {
			return &ErrDomainUsed{LeaserGroup: group}
		}
	}

	ci.clearGroup(group)

	logger := internal.SubLogger(ctx, "ingress")
	for domain, addresses := range domainAddresses {
		ci.domainsByGroup[group] = append(ci.domainsByGroup[group], domain)
		ci.addressesByDomain[domain] = address{
			group: group,
			list:  addresses,
		}
		logger.Info("registering route", zap.String("domain", domain), zap.Strings("addresses", addresses), zap.String("group", group))
	}

	return ci.updateState(ctx)
}

func (ci *cachedIngress) clearGroup(group string) {
	for _, domain := range ci.domainsByGroup[group] {
		delete(ci.addressesByDomain, domain)
	}
	delete(ci.domainsByGroup, group)
}

func (ci *cachedIngress) updateState(ctx context.Context) error {
	newState := make([]Record, 0, len(ci.addressesByDomain))
	for domain, addresses := range ci.addressesByDomain {
		newState = append(newState, Record{
			Domain:    domain,
			Addresses: addresses.list,
			Group:     addresses.group,
		})
	}
	return ci.backend.Set(ctx, newState)
}

type address struct {
	group string
	list  []string
}

type ErrDomainUsed struct {
	LeaserGroup string
}

func (edu *ErrDomainUsed) Error() string {
	return "domain already used by group " + edu.LeaserGroup
}

// AsErrDomainUsed tries convert arbitrary error as corresponded error type.
// Returns true only in case conversion successful.
func AsErrDomainUsed(err error) (*ErrDomainUsed, bool) {
	var edu *ErrDomainUsed
	return edu, errors.As(err, &edu)
}
