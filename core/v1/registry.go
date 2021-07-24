package v1

import (
	"errors"
	"strings"
	"sync"

	"github.com/reddec/git-pipe/core"
	"go.uber.org/zap"
)

var (
	ErrServiceAlreadyRegistered = errors.New("service already registered with the same name in the namespace")
	ErrServiceNotRegistered     = errors.New("service not registered")
	ErrServiceDomainAlreadyUsed = errors.New("service domain already used")
)

func NewRegistry(rootDomain string) core.Registry {
	return &registry{
		namespaces: map[string]*namespace{},
		byDomain:   map[string]core.Service{},
		rootDomain: rootDomain,
	}
}

type registry struct {
	lock       sync.RWMutex
	rootDomain string
	namespaces map[string]*namespace
	byDomain   map[string]core.Service
	listeners  map[core.RegistryEventStream]chan core.RegistryEventMessage
}

type namespace struct {
	name     string
	services map[string]core.Service
}

func (reg *registry) Domain() string {
	return reg.rootDomain
}

func (reg *registry) Subscribe(buffer int, replay bool) core.RegistryEventStream {
	reg.lock.Lock()
	defer reg.lock.Unlock()
	ch := make(chan core.RegistryEventMessage, buffer)
	reg.listeners[ch] = ch
	if replay {
		reg.replay(ch)
	}
	return ch
}

func (reg *registry) Unsubscribe(ch core.RegistryEventStream) {
	reg.lock.Lock()
	defer reg.lock.Unlock()
	if old, ok := reg.listeners[ch]; ok {
		close(old)
	}
	delete(reg.listeners, ch)
}

func (reg *registry) Register(srv core.Service) error {
	reg.lock.Lock()
	defer reg.lock.Unlock()
	domain := reg.getDomain(srv)
	// check domain
	if _, exists := reg.byDomain[domain]; exists {
		return ErrServiceDomainAlreadyUsed
	}
	srv.Domain = domain

	ns, ok := reg.namespaces[srv.Namespace]
	if !ok {
		ns = &namespace{name: srv.Namespace, services: map[string]core.Service{}}
		reg.namespaces[srv.Namespace] = ns
	}

	_, exists := ns.services[srv.Name]
	if exists {
		return ErrServiceAlreadyRegistered
	}

	// index service
	ns.services[srv.Name] = srv
	reg.byDomain[domain] = srv
	reg.notify(core.RegistryEventRegistered, srv)
	return nil
}

func (reg *registry) Unregister(namespace, name string) {
	reg.lock.Lock()
	defer reg.lock.Unlock()
	ns, ok := reg.namespaces[namespace]
	if !ok {
		return
	}
	srv, ok := ns.services[name]
	if !ok {
		return
	}
	delete(ns.services, name)
	delete(reg.byDomain, reg.getDomain(srv))
	reg.notify(core.RegistryEventUnregistered, srv)
}

func (reg *registry) Find(namespace, name string) (core.Service, error) {
	reg.lock.RLock()
	defer reg.lock.RUnlock()
	if ns, ok := reg.namespaces[namespace]; ok {
		if srv, ok := ns.services[name]; ok {
			return srv, nil
		}
	}
	return core.Service{}, ErrServiceNotRegistered
}

func (reg *registry) Lookup(domain string) (core.Service, error) {
	domain = reg.normalizeDomain(domain)
	reg.lock.RLock()
	defer reg.lock.RUnlock()

	srv, ok := reg.byDomain[domain]
	if !ok {
		return core.Service{}, ErrServiceNotRegistered
	}
	return srv, nil
}

func (reg *registry) All() []core.Service {
	cp := make([]core.Service, 0, len(reg.byDomain))
	reg.lock.RLock()
	defer reg.lock.RUnlock()
	for _, srv := range reg.byDomain {
		cp = append(cp, srv)
	}
	return cp
}

func (reg *registry) notify(event core.RegistryEvent, service core.Service) {
	msg := core.RegistryEventMessage{
		Event:   event,
		Service: service,
	}
	for _, ch := range reg.listeners {
		select {
		case ch <- msg:
		default:
			zap.L().Warn("overflow events in registry")
		}
	}
}

func (reg *registry) replay(to chan core.RegistryEventMessage) {
	for _, srv := range reg.byDomain {
		select {
		case to <- core.RegistryEventMessage{
			Event:   core.RegistryEventRegistered,
			Service: srv,
		}:
		default:
			return
		}
	}
}

func (reg *registry) getDomain(srv core.Service) string {
	var zone = srv.Domain
	if srv.Domain == "" {
		zone = srv.Name + "." + srv.Namespace
	}
	return reg.normalizeDomain(zone)
}

func (reg *registry) normalizeDomain(domain string) string {
	if reg.rootDomain != "" && !strings.HasSuffix(domain, "."+reg.rootDomain) {
		domain += "." + reg.rootDomain
	}
	return domain
}
