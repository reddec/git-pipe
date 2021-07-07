package embedded

import (
	"bytes"
	"context"
	_ "embed" // for index template
	"fmt"
	"html/template"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"

	"github.com/reddec/git-pipe/internal"
	"github.com/reddec/git-pipe/packs"
)

type Config struct {
	PathRouting bool // not yet supported - path based routing instead of domains
	Port        int  // binding port, used only for index page
	Index       bool // show index in case of not found domain
}

func New(cfg Config) *Embedded {
	return &Embedded{
		config:          cfg,
		proxiesByDomain: map[string][]http.Handler{},
		domainsByGroup:  map[string][]string{},
	}
}

type Embedded struct {
	config          Config
	indexPage       string
	lock            sync.RWMutex
	domainsByGroup  map[string][]string
	proxiesByDomain map[string][]http.Handler
}

func (ed *Embedded) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()

	logger := internal.SubLogger(request.Context(), "http-embedded")

	domain, _, _ := net.SplitHostPort(request.Host)
	if domain == "" {
		domain = request.Host
	}
	logger.Println(domain, request.Method, request.RequestURI)
	ed.lock.RLock()
	proxies, ok := ed.proxiesByDomain[domain]
	ed.lock.RUnlock()
	if !ok {
		if ed.config.Index {
			writer.Header().Set("Content-Type", "text/html")
			writer.WriteHeader(http.StatusNotFound)
			_, _ = writer.Write([]byte(ed.indexPage))
		} else {
			http.NotFound(writer, request)
		}

		return
	}

	proxy := proxies[rand.Int()%len(proxies)] // nolint:gosec
	proxy.ServeHTTP(writer, request)
}

func (ed *Embedded) Update(ctx context.Context, group string, services []packs.Service) error {
	indexPage, err := generateIndex(ed.config, services)
	if err != nil {
		return fmt.Errorf("generate index: %w", err)
	}
	ed.lock.Lock()
	defer ed.lock.Unlock()
	ed.indexPage = indexPage
	// remove old index
	for _, oldDomain := range ed.domainsByGroup[group] {
		delete(ed.proxiesByDomain, oldDomain)
	}
	delete(ed.domainsByGroup, group)

	logger := internal.SubLogger(ctx, "router")
	// add new index
	for _, srv := range services {
		domain := srv.Domain

		var handlers []http.Handler
		for _, addr := range srv.Addresses {
			u, err := url.Parse("http://" + addr)
			if err != nil {
				return fmt.Errorf("parse service url: %w", err)
			}
			handlers = append(handlers, httputil.NewSingleHostReverseProxy(u))
		}

		ed.proxiesByDomain[domain] = handlers
		ed.domainsByGroup[group] = append(ed.domainsByGroup[group], domain)
		logger.Println(domain, "->", strings.Join(srv.Addresses, ","))
	}
	return nil
}

//go:embed index.html
var indexTemplate string // nolint:gochecknoglobals

func generateIndex(config Config, services []packs.Service) (string, error) {
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

	if !config.PathRouting {
		params.Prefix = "//"
	}

	params.Services = services
	params.Config = config
	params.ByGroup = createIndex(services)

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
