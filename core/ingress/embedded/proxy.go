package embedded

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/reddec/git-pipe/internal"
	"go.uber.org/zap"
)

type Resolver interface {
	// Resolve address or address with port to routable (from application) endpoint (with port if needed).
	Resolve(ctx context.Context, address string) (string, error)
}

// Proxy request to endpoint. Nil resolver disable address resolution.
func Proxy(resolver Resolver) RouteHandler {
	return RouteHandlerFunc(func(writer http.ResponseWriter, request *http.Request, record Route) error {
		address := record.Record.Addresses[rand.Int()%len(record.Record.Addresses)]

		ctx := request.Context()

		var endpoint = address
		if resolver != nil {
			dest, err := resolver.Resolve(ctx, address)
			if err != nil {
				return fmt.Errorf("resolve address: %w", err)
			}
			endpoint = dest
		}

		logger := internal.LoggerFromContext(ctx).With(zap.String("address", address), zap.String("endpoint", endpoint))
		logger.Debug("proxy endpoint resolved")

		request.WithContext(internal.WithLogger(ctx, logger))

		u, err := url.Parse("http://" + endpoint)
		if err != nil {
			return fmt.Errorf("parse URL: %w", err)
		}
		httputil.NewSingleHostReverseProxy(u).ServeHTTP(writer, request)
		return ErrAbort
	})
}
