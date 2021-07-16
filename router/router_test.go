package router_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/reddec/git-pipe/packs"
	"github.com/reddec/git-pipe/router"
	"github.com/stretchr/testify/assert"
)

func TestRouter_ServeHTTP(t *testing.T) {
	t.Run("basic domain routing", func(t *testing.T) {
		rt := router.New(router.Config{})

		rt.Update("test", []packs.Service{
			{
				Group:  "my-repo",
				Name:   "app",
				Domain: "app.example.com",
			},
		})

		var routed bool
		rt.Handle(router.RouteHandlerFunc(func(writer http.ResponseWriter, request *http.Request, route *router.Route) error {
			routed = true
			return nil
		}))

		rr := httptest.NewRecorder()
		rq := httptest.NewRequest(http.MethodGet, "https://app.example.com/x/y/z", nil)
		rt.ServeHTTP(rr, rq)
		assert.True(t, routed)
		assert.Equal(t, http.StatusOK, rr.Code)
	})
	t.Run("unknown domain returns 404", func(t *testing.T) {
		rt := router.New(router.Config{Index: true})

		rt.Update("test", []packs.Service{
			{
				Group:  "my-repo",
				Name:   "app",
				Domain: "app.example.com",
			},
		})

		var routed bool
		rt.Handle(router.RouteHandlerFunc(func(writer http.ResponseWriter, request *http.Request, route *router.Route) error {
			routed = true
			return nil
		}))

		rr := httptest.NewRecorder()
		rq := httptest.NewRequest(http.MethodGet, "https://app1.example.com/x/y/z", nil)
		rt.ServeHTTP(rr, rq)
		assert.False(t, routed)
		assert.Equal(t, http.StatusNotFound, rr.Code)
		assert.Contains(t, rr.Header().Get("Content-Type"), "text/html")
	})
	t.Run("basic path routing", func(t *testing.T) {
		rt := router.New(router.Config{
			PathRouting: true,
		})

		rt.Update("test", []packs.Service{
			{
				Group:  "my-repo",
				Name:   "app",
				Domain: "app.example.com",
			},
		})

		var routed bool
		rt.Handle(router.RouteHandlerFunc(func(writer http.ResponseWriter, request *http.Request, route *router.Route) error {
			routed = true
			return nil
		}))

		rr := httptest.NewRecorder()
		rq := httptest.NewRequest(http.MethodGet, "https://nomatter.example.com/app.example.com/some/path", nil)
		rt.ServeHTTP(rr, rq)
		assert.True(t, routed)
		assert.Equal(t, http.StatusOK, rr.Code)
	})
	t.Run("path routing should trim source prefix", func(t *testing.T) {
		rt := router.New(router.Config{
			PathRouting: true,
		})

		rt.Update("test", []packs.Service{
			{
				Group:  "my-repo",
				Name:   "app",
				Domain: "app.example.com",
			},
		})

		var routed bool
		rt.Handle(router.RouteHandlerFunc(func(writer http.ResponseWriter, request *http.Request, route *router.Route) error {
			routed = true
			assert.Equal(t, "/some/path", request.URL.Path)
			return nil
		}))

		rr := httptest.NewRecorder()
		rq := httptest.NewRequest(http.MethodGet, "https://nomatter.example.com/app.example.com/some/path", nil)
		rt.ServeHTTP(rr, rq)
		assert.True(t, routed)
		assert.Equal(t, http.StatusOK, rr.Code)
	})
}

func TestRandom(t *testing.T) {
	var routed atomic.Value
	routed.Store(false)

	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		routed.Store(true)
		assert.Equal(t, "/x/y/z", request.URL.Path)
	}))
	defer srv.Close()

	rt := router.New(router.Config{})
	rt.Update("test", []packs.Service{
		{
			Group:  "my-repo",
			Name:   "app",
			Domain: "app.example.com",
			Addresses: []string{
				srv.Listener.Addr().String(),
			},
		},
	})

	rt.Handle(&router.Random{})

	rr := httptest.NewRecorder()
	rq := httptest.NewRequest(http.MethodGet, "https://app.example.com/x/y/z", nil)
	rt.ServeHTTP(rr, rq)
	assert.True(t, routed.Load().(bool))
	assert.Equal(t, http.StatusOK, rr.Code)
}
