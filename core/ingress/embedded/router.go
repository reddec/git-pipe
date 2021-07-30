package embedded

import (
	"context"
	_ "embed" // for not-found page resource
	"encoding/hex"
	"errors"
	"html/template"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/reddec/git-pipe/core/ingress"
	"github.com/reddec/git-pipe/internal"
	"go.uber.org/zap"
)

// New router which can be used independently or as backend for ingress.
// If rootDomain defined it will be added to all domains in records during Set operation in case path routing disabled.
func New(requestResolver RequestResolver, chain ...RouteHandler) *Router {
	return &Router{
		chain:    chain,
		resolver: requestResolver,
	}
}

type Router struct {
	chain        []RouteHandler
	routes       atomic.Value // map[string]ingress.Record
	resolver     RequestResolver
	disableIndex bool
}

// Index page visibility.
func (rt *Router) Index(enable bool) {
	rt.disableIndex = !enable
}

// Domains for all known records.
func (rt *Router) Domains() map[string]bool {
	v, _ := rt.routes.Load().(map[string]ingress.Record)
	var ans = make(map[string]bool)
	for k := range v {
		ans[k] = true
	}
	return ans
}

// Set routing tables.
func (rt *Router) Set(ctx context.Context, records []ingress.Record) error {
	var index = make(map[string]ingress.Record)

	for _, rec := range records {
		index[rt.resolver.FQDN(rec.Domain)] = rec
	}
	rt.routes.Store(index)
	return nil
}

func (rt *Router) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	routes, ok := rt.routes.Load().(map[string]ingress.Record)
	if !ok {
		rt.notFound(writer, request)
		return
	}
	domain := rt.resolver.Domain(request)

	record, ok := routes[domain]
	if !ok {
		rt.notFound(writer, request)
		return
	}
	rt.serveRoute(writer, request, record)
}

func (rt *Router) notFound(writer http.ResponseWriter, request *http.Request) {
	if rt.disableIndex {
		http.NotFound(writer, request)
		return
	}
	rt.showIndex(writer, request)
}

//go:embed index.html
var templateContent string //nolint:gochecknoglobals

func (rt *Router) showIndex(writer http.ResponseWriter, _ *http.Request) {
	t, err := template.New("").Funcs(template.FuncMap{
		"fqdn": rt.resolver.FQDN,
		"url":  rt.resolver.URL,
	}).Parse(templateContent)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}

	routes, _ := rt.routes.Load().(map[string]ingress.Record)
	var byGroup = make(map[string][]ingress.Record)

	for _, record := range routes {
		byGroup[record.Group] = append(byGroup[record.Group], record)
	}

	writer.Header().Set("Content-Type", "text/html")
	writer.WriteHeader(http.StatusNotFound)

	_ = t.Execute(writer, map[string]interface{}{
		"ByGroup":  byGroup,
		"ByDomain": routes,
	})
}

func (rt *Router) serveRoute(writer http.ResponseWriter, request *http.Request, record ingress.Record) {
	started := time.Now()
	ctx := request.Context()
	correlationID := request.Header.Get("X-Correlation-ID")
	requestID := request.Header.Get("X-Request-ID")

	if requestID == "" {
		id := uuid.New()
		requestID = hex.EncodeToString(id[:])
	}

	logger := internal.SubLogger(ctx, "router").With(
		zap.String("method", request.Method),
		zap.String("client_addr", request.RemoteAddr),
		zap.String("url", request.URL.Redacted()),
		zap.String("domain", record.Domain),
		zap.String("correlation_id", correlationID),
		zap.String("request_id", requestID),
		zap.String("group", record.Group),
	)

	wrapped := &writeWrapper{ResponseWriter: writer, protectedHeaders: map[string]string{
		"X-Correlation-ID": correlationID, // Correlation ID always should be the same as in request
	}}

	var newWriter http.ResponseWriter = wrapped

	if hj, ok := writer.(http.Hijacker); ok {
		newWriter = &hijackedWrapper{
			writeWrapper: *wrapped,
			Hijacker:     hj,
		}
	}

	logger.Debug("request started")
	defer func() {
		logger.Info("request finished", zap.Int("code", wrapped.statusCode), zap.Duration("duration", time.Since(started)))
	}()

	request = request.WithContext(internal.WithLogger(ctx, logger))

	route := Route{
		RequestID: requestID,
		Record:    record,
	}
	for _, handler := range rt.chain {
		err := handler.ServeRoute(newWriter, request, route)
		if errors.Is(err, ErrAbort) {
			break
		}
		if err != nil {
			newWriter.WriteHeader(http.StatusInternalServerError)
			logger.Error("failed to process", zap.Error(err))
			break
		}
	}

	if !wrapped.statusWritten {
		wrapped.WriteHeader(http.StatusNoContent)
	}
}

var ErrAbort = errors.New("abort")

type Route struct {
	Record    ingress.Record
	RequestID string
}

type RouteHandler interface {
	ServeRoute(writer http.ResponseWriter, request *http.Request, record Route) error
}

type RouteHandlerFunc func(writer http.ResponseWriter, request *http.Request, record Route) error

func (rhf RouteHandlerFunc) ServeRoute(writer http.ResponseWriter, request *http.Request, record Route) error {
	return rhf(writer, request, record)
}

type writeWrapper struct {
	protectedHeaders map[string]string
	statusCode       int
	statusWritten    bool
	http.ResponseWriter
}

func (w *writeWrapper) Write(bytes []byte) (int, error) {
	if !w.statusWritten {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(bytes)
}

func (w *writeWrapper) WriteHeader(statusCode int) {
	if !w.statusWritten {
		w.statusWritten = true
		w.statusCode = statusCode
	}
	for k, v := range w.protectedHeaders {
		w.ResponseWriter.Header().Set(k, v)
	}
	w.ResponseWriter.WriteHeader(statusCode)
}

type hijackedWrapper struct {
	writeWrapper
	http.Hijacker
}
