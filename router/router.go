package router

import (
	"bytes"
	_ "embed" // for index template
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/reddec/git-pipe/internal"
	"github.com/reddec/git-pipe/packs"
)

var ErrAbort = errors.New("abort")

type Route struct {
	Group   string
	Service packs.Service
}

type RouteHandler interface {
	ServeRoute(writer http.ResponseWriter, request *http.Request, route *Route) error
}

type RouteHandlerFunc func(writer http.ResponseWriter, request *http.Request, route *Route) error

func (rhf RouteHandlerFunc) ServeRoute(writer http.ResponseWriter, request *http.Request, route *Route) error {
	return rhf(writer, request, route)
}

type Config struct {
	PathRouting bool // path based routing instead of domains
	Port        int  // binding port, used only for index page
	Index       bool // show index in case of not found domain
}

func New(cfg Config) *Router {
	return &Router{
		config:          cfg,
		serviceByDomain: map[string]packs.Service{},
		domainsByGroup:  map[string][]string{},
	}
}

// Router for incoming request.
// Detects service by domain or path.
type Router struct {
	chains          []RouteHandler
	config          Config
	lock            sync.RWMutex
	serviceByDomain map[string]packs.Service
	domainsByGroup  map[string][]string
}

// HasDomain reports true in case domain is registered in the router. Thread safe.
func (ed *Router) HasDomain(domain string) bool {
	ed.lock.RLock()
	defer ed.lock.RUnlock()
	_, ok := ed.serviceByDomain[domain]
	return ok
}

// Handle routed request. Thread UNSAFE.
func (ed *Router) Handle(handler RouteHandler) {
	ed.chains = append(ed.chains, handler)
}

func (ed *Router) ServeRoute(writer http.ResponseWriter, request *http.Request, route *Route) error {
	for _, c := range ed.chains {
		err := c.ServeRoute(writer, request, route)
		if err != nil {
			return err
		}
	}
	return nil
}

func (ed *Router) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()

	ctx := request.Context()
	logger := internal.SubLogger(ctx, "http-embedded")
	request = request.WithContext(internal.WithLogger(ctx, logger))

	domain := ed.getDomain(request)
	logger.Println(domain, request.Method, request.RequestURI)
	ed.lock.RLock()
	srv, ok := ed.serviceByDomain[domain]
	ed.lock.RUnlock()

	if !ok {
		ed.showIndexPage(writer, request)
		return
	}

	if ed.config.PathRouting {
		request.URL.Path = request.URL.Path[len(domain)+1:]
	}

	err := ed.ServeRoute(writer, request, &Route{
		Group:   srv.Group,
		Service: srv,
	})
	if err != nil && !errors.Is(err, ErrAbort) {
		writer.WriteHeader(http.StatusInternalServerError)
		logger.Println("failed to process:", err, "service:", srv.Name, "group:", srv.Group)
	}
}

// Update routing table for services in group.
func (ed *Router) Update(group string, services []packs.Service) {
	ed.lock.Lock()
	defer ed.lock.Unlock()
	// remove old index
	for _, oldDomain := range ed.domainsByGroup[group] {
		delete(ed.serviceByDomain, oldDomain)
	}
	delete(ed.domainsByGroup, group)

	// add new index
	for _, srv := range services {
		ed.serviceByDomain[srv.Domain] = srv
		ed.domainsByGroup[group] = append(ed.domainsByGroup[group], srv.Domain)
	}
}

func (ed *Router) getDomain(request *http.Request) string {
	if ed.config.PathRouting {
		for _, path := range strings.SplitN(request.URL.Path, "/", 3) { //nolint:gomnd
			if len(path) != 0 {
				return path
			}
		}
		return ""
	}
	domain, _, _ := net.SplitHostPort(request.Host)
	if domain == "" {
		domain = request.Host
	}
	return domain
}

func (ed *Router) showIndexPage(writer http.ResponseWriter, request *http.Request) {
	if !ed.config.Index {
		http.NotFound(writer, request)
		return
	}
	page, err := ed.renderIndexPage()
	if err != nil {
		internal.LoggerFromContext(request.Context()).Println("render index page failed:", err)
		http.NotFound(writer, request)
		return
	}
	writer.Header().Set("Content-Type", "text/html")
	writer.WriteHeader(http.StatusNotFound)
	_, _ = writer.Write([]byte(page))
}

//go:embed index.html
var indexTemplate string // nolint:gochecknoglobals

func (ed *Router) renderIndexPage() (string, error) {
	tpl, err := template.New("").Parse(indexTemplate)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var params struct {
		Services []packs.Service
		ByGroup  map[string]*group
		Config   Config
		Prefix   string
		Logo     string
	}

	if !ed.config.PathRouting {
		params.Prefix = "//"
	}

	ed.lock.RLock()
	var services = make([]packs.Service, 0, len(ed.serviceByDomain))
	for _, srv := range ed.serviceByDomain {
		services = append(services, srv)
	}
	byGroup := createIndex(services)
	ed.lock.RUnlock()

	params.Services = services
	params.Config = ed.config
	params.ByGroup = byGroup

	var data bytes.Buffer
	err = tpl.Execute(&data, params)

	return data.String(), err
}

type group struct {
	Name     string
	Services map[string][]packs.Service
}

func createIndex(services []packs.Service) map[string]*group {
	var ans = make(map[string]*group)
	for _, srv := range services {
		g, ok := ans[srv.Group]
		if !ok {
			g = &group{Name: srv.Group, Services: map[string][]packs.Service{}}
			ans[srv.Group] = g
		}

		g.Services[srv.Name] = append(g.Services[srv.Name], srv)
	}

	return ans
}
