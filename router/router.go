package router

import (
	"bytes"
	"context"
	_ "embed" // for index template
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"strings"

	"github.com/reddec/git-pipe/core"
	"github.com/reddec/git-pipe/internal"
	"go.uber.org/zap"
	"golang.org/x/crypto/acme/autocert"
)

var ErrAbort = errors.New("abort")

type Route struct {
	Service     core.Service
	Environment core.Environment
}

type RouteHandler interface {
	ServeRoute(writer http.ResponseWriter, request *http.Request, route *Route) error
}

type RouteHandlerFunc func(writer http.ResponseWriter, request *http.Request, route *Route) error

func (rhf RouteHandlerFunc) ServeRoute(writer http.ResponseWriter, request *http.Request, route *Route) error {
	return rhf(writer, request, route)
}

type Config struct {
	Bind        string `long:"bind" short:"b" env:"BIND" description:"Address to where bind HTTP server" default:"127.0.0.1:8080"`
	AutoTLS     bool   `long:"auto-tls" short:"T" env:"AUTO_TLS" description:"Automatic TLS (Let's Encrypt), ignores bind address and uses 0.0.0.0:443 port"`
	TLS         bool   `long:"tls" env:"TLS" description:"Enable HTTPS serving with TLS. TLS files should support multiple domains, otherwise path-routing should be enabled. Ignored with --auto-tls'" json:"tls"`
	SSLDir      string `long:"ssl-dir" env:"SSL_DIR" description:"Directory for SSL certificates and keys. Should contain server.{crt,key} files unless auto-tls enabled. For auto-tls it is used as cache dir" default:"ssl"`
	NoIndex     bool   `long:"no-index" env:"NO_INDEX" description:"Disable index page"`
	PathRouting bool   `long:"path-routing" short:"P" env:"PATH_ROUTING" description:"Enable path routing instead of domain-based. Implicitly disables --domain"`
}

func New(cfg Config, handlers ...RouteHandler) core.Daemon {
	if cfg.AutoTLS {
		return autoTLSServer(cfg, handlers...)
	}
	if cfg.TLS {
		return tlsServer(cfg, handlers...)
	}
	return plainServer(cfg, handlers...)
}

func plainServer(cfg Config, handlers ...RouteHandler) core.Daemon {
	return core.FuncDaemon(func(global context.Context, environment core.DaemonEnvironment) error {
		ctx, cancel := context.WithCancel(global)
		defer cancel()
		server := NewHTTPServer(ctx, cfg, environment.Global(), handlers...)

		go func() {
			<-ctx.Done()
			if err := server.Close(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				internal.LoggerFromContext(global).Warn("close failed", zap.Error(err))
			}
		}()

		environment.Ready()
		return server.ListenAndServe()
	})
}

func tlsServer(cfg Config, handlers ...RouteHandler) core.Daemon {
	panic("TODO: implement") // TODO:
	return core.FuncDaemon(func(global context.Context, environment core.DaemonEnvironment) error {
		ctx, cancel := context.WithCancel(global)
		defer cancel()

		server := NewHTTPServer(ctx, cfg, environment.Global(), handlers...)

		go func() {
			<-ctx.Done()
			if err := server.Close(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				internal.LoggerFromContext(ctx).Warn("close failed", zap.Error(err))
			}
		}()

		environment.Ready()
		return server.ListenAndServe()
	})
}

func autoTLSServer(cfg Config, handlers ...RouteHandler) core.Daemon {
	return core.FuncDaemon(func(global context.Context, environment core.DaemonEnvironment) error {
		ctx, cancel := context.WithCancel(global)
		defer cancel()

		server := NewHTTPServer(ctx, cfg, environment.Global(), handlers...)
		manager := &autocert.Manager{
			Prompt: autocert.AcceptTOS,
			Cache:  autocert.DirCache(cfg.SSLDir),
			HostPolicy: func(ctx context.Context, host string) error {
				if cfg.PathRouting && host == environment.Global().Registry().Domain() {
					return nil
				}
				_, err := environment.Global().Registry().Lookup(host)
				return err
			},
		}

		listener := manager.Listener()

		environment.Ready()

		go func() {
			<-ctx.Done()
			if err := server.Close(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				internal.LoggerFromContext(ctx).Warn("close failed", zap.Error(err))
			}
		}()

		return server.Serve(listener)
	})
}

func NewHTTPServer(ctx context.Context, cfg Config, environment core.Environment, handlers ...RouteHandler) *http.Server {
	handler := newRouter(cfg, environment, append(handlers, &Random{})...)

	return &http.Server{
		Addr:     cfg.Bind,
		Handler:  handler,
		ErrorLog: zap.NewStdLog(internal.SubLogger(ctx, "http-server")),
		BaseContext: func(listener net.Listener) context.Context {
			return ctx
		},
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			return ctx
		},
	}
}

func newRouter(cfg Config, env core.Environment, handlers ...RouteHandler) *router {
	return &router{
		config:      cfg,
		chain:       handlers,
		environment: env,
	}
}

// router for incoming request.
// Detects service by domain or path.
type router struct {
	environment core.Environment
	chain       []RouteHandler
	config      Config
}

func (ed *router) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	defer request.Body.Close()

	ctx := request.Context()
	logger := internal.SubLogger(ctx, "router").With(
		zap.String("method", request.Method),
		zap.String("remote", request.RemoteAddr),
		zap.String("url", request.URL.Redacted()),
	)

	domain := ed.getDomain(request)
	logger.Info("request started", zap.String("domain", domain))

	srv, err := ed.environment.Registry().Lookup(domain)
	if err != nil {
		logger.Warn("failed to find service by domain", zap.Error(err))
		ed.showIndexPage(writer, request)
		return
	}

	logger = logger.With(zap.String("namespace", srv.Namespace), zap.String("name", srv.Name))
	if ed.config.PathRouting {
		request.URL.Path = request.URL.Path[len(domain)+1:]
	}
	logger.Info("route found", zap.String("path", request.URL.Path))

	request = request.WithContext(internal.WithLogger(ctx, logger))

	err = ed.invokeChain(writer, request, &Route{
		Service:     srv,
		Environment: ed.environment,
	})
	if err != nil && !errors.Is(err, ErrAbort) {
		writer.WriteHeader(http.StatusInternalServerError)
		logger.Error("failed to process", zap.Error(err))
	}
}

func (ed *router) invokeChain(writer http.ResponseWriter, request *http.Request, route *Route) error {
	for _, c := range ed.chain {
		err := c.ServeRoute(writer, request, route)
		if err != nil {
			return err
		}
	}
	return nil
}

func (ed *router) getDomain(request *http.Request) string {
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

func (ed *router) showIndexPage(writer http.ResponseWriter, request *http.Request) {
	if ed.config.NoIndex {
		http.NotFound(writer, request)
		return
	}
	page, err := ed.renderIndexPage()
	if err != nil {
		internal.LoggerFromContext(request.Context()).Error("render index page failed", zap.Error(err))
		http.NotFound(writer, request)
		return
	}
	writer.Header().Set("Content-Type", "text/html")
	writer.WriteHeader(http.StatusNotFound)
	_, _ = writer.Write([]byte(page))
}

//go:embed index.html
var indexTemplate string // nolint:gochecknoglobals

func (ed *router) renderIndexPage() (string, error) {
	tpl, err := template.New("").Parse(indexTemplate)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var params struct {
		Services []core.Service
		ByGroup  map[string]*group
		Config   Config
		Prefix   string
		Logo     string
	}

	if !ed.config.PathRouting {
		params.Prefix = "//"
	}

	services := ed.environment.Registry().All()
	byGroup := createIndex(services)

	params.Services = services
	params.Config = ed.config
	params.ByGroup = byGroup

	var data bytes.Buffer
	err = tpl.Execute(&data, params)

	return data.String(), err
}

type group struct {
	Name     string
	Services map[string][]core.Service
}

func createIndex(services []core.Service) map[string]*group {
	var ans = make(map[string]*group)
	for _, srv := range services {
		g, ok := ans[srv.Namespace]
		if !ok {
			g = &group{Name: srv.Namespace, Services: map[string][]core.Service{}}
			ans[srv.Namespace] = g
		}

		g.Services[srv.Name] = append(g.Services[srv.Name], srv)
	}

	return ans
}
