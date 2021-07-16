package router

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
)

// Random distribution to backend nodes. Based on service addresses.
type Random struct {
	random interface {
		Int() int
	}
}

func (rnd *Random) ServeRoute(writer http.ResponseWriter, request *http.Request, route *Route) error {
	if len(route.Service.Addresses) == 0 {
		http.Error(writer, "backend address not found", http.StatusBadGateway)
		return ErrAbort
	}
	addr := route.Service.Addresses[rnd.int()%len(route.Service.Addresses)]
	u, err := url.Parse("http://" + addr)
	if err != nil {
		return fmt.Errorf("parse backend URL: %w", err)
	}

	httputil.NewSingleHostReverseProxy(u).ServeHTTP(writer, request)
	return nil
}

func (rnd *Random) int() int {
	if v := rnd.random; v != nil {
		return v.Int()
	}
	return rand.Int() //nolint:gosec
}
