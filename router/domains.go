package router

import (
	"context"
	"sync"

	"github.com/reddec/git-pipe/packs"
)

func WithState(next Router) *Domains {
	return &Domains{
		groupByDomain:  map[string]string{},
		domainsByGroup: map[string][]string{},
		next:           next,
	}
}

type Domains struct {
	lock           sync.RWMutex
	groupByDomain  map[string]string
	domainsByGroup map[string][]string
	next           Router
}

func (d *Domains) Update(ctx context.Context, group string, services []packs.Service) error {
	err := d.next.Update(ctx, group, services)

	d.lock.Lock()
	defer d.lock.Unlock()
	for _, domain := range d.domainsByGroup[group] {
		delete(d.groupByDomain, domain)
	}
	delete(d.domainsByGroup, group)

	var domains = make([]string, 0, len(services))
	for _, srv := range services {
		d.groupByDomain[srv.Domain] = group
		domains = append(domains, srv.Domain)
	}

	d.domainsByGroup[group] = domains
	return err
}

func (d *Domains) HasDomain(domain string) bool {
	d.lock.RLock()
	defer d.lock.RUnlock()
	_, ok := d.groupByDomain[domain]
	return ok
}
