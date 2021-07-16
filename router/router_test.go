package router_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/dgrijalva/jwt-go"
	"github.com/reddec/git-pipe/packs"
	"github.com/reddec/git-pipe/router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestJWT(t *testing.T) {
	t.Run("valid token should work", func(t *testing.T) {
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"aud": "client1"}).SignedString([]byte("qwerty"))
		require.NoError(t, err)
		assert.NotEmpty(t, token)

		rt := router.New(router.Config{})

		rt.Update("test", []packs.Service{
			{
				Group:  "my-repo",
				Name:   "app",
				Domain: "app.example.com",
			},
		})
		rt.Handle(router.JWT("qwerty"))
		rt.Handle(router.RouteHandlerFunc(func(writer http.ResponseWriter, request *http.Request, route *router.Route) error {
			assert.Equal(t, "client1", request.Header.Get(router.HeaderUser))
			return nil
		}))

		t.Run("token in header", func(t *testing.T) {
			rr := httptest.NewRecorder()
			rq := httptest.NewRequest(http.MethodGet, "https://app.example.com/x/y/z", nil)
			rq.Header.Set("Authorization", "Bearer "+token)
			rt.ServeHTTP(rr, rq)

			if !assert.Equal(t, http.StatusOK, rr.Code) {
				t.Log(rr.Body.String())
			}
		})
		t.Run("token in query", func(t *testing.T) {
			rr := httptest.NewRecorder()
			rq := httptest.NewRequest(http.MethodGet, "https://app.example.com/x/y/z?token="+token, nil)
			rt.ServeHTTP(rr, rq)

			if !assert.Equal(t, http.StatusOK, rr.Code) {
				t.Log(rr.Body.String())
			}
		})

	})

	t.Run("restrict domain", func(t *testing.T) {
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"aud": "client1",
			"sub": "my-repo",
		}).SignedString([]byte("qwerty"))
		require.NoError(t, err)
		assert.NotEmpty(t, token)

		rt := router.New(router.Config{})

		rt.Update("test", []packs.Service{
			{Group: "my-repo", Name: "app", Domain: "app.example.com"},
			{Group: "my-repo-2", Name: "app2", Domain: "app2.example.com"},
		})
		rt.Handle(router.JWT("qwerty"))
		rt.Handle(router.RouteHandlerFunc(func(writer http.ResponseWriter, request *http.Request, route *router.Route) error {
			return nil
		}))

		t.Run("ok with allowed", func(t *testing.T) {
			rr := httptest.NewRecorder()
			rq := httptest.NewRequest(http.MethodGet, "https://app.example.com/x/y/z", nil)
			rq.Header.Set("Authorization", "Bearer "+token)
			rt.ServeHTTP(rr, rq)

			if !assert.Equal(t, http.StatusOK, rr.Code) {
				t.Log(rr.Body.String())
			}
		})
		t.Run("no-no for another", func(t *testing.T) {
			rr := httptest.NewRecorder()
			rq := httptest.NewRequest(http.MethodGet, "https://app2.example.com/x/y/z", nil)
			rq.Header.Set("Authorization", "Bearer "+token)
			rt.ServeHTTP(rr, rq)

			assert.Equal(t, http.StatusForbidden, rr.Code)
		})
	})

	t.Run("restrict method", func(t *testing.T) {
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"aud":     "client1",
			"methods": []string{"POST"},
		}).SignedString([]byte("qwerty"))
		require.NoError(t, err)
		assert.NotEmpty(t, token)

		rt := router.New(router.Config{})

		rt.Update("test", []packs.Service{
			{Group: "my-repo", Name: "app", Domain: "app.example.com"},
		})
		rt.Handle(router.JWT("qwerty"))
		rt.Handle(router.RouteHandlerFunc(func(writer http.ResponseWriter, request *http.Request, route *router.Route) error {
			return nil
		}))

		t.Run("ok with POST", func(t *testing.T) {
			rr := httptest.NewRecorder()
			rq := httptest.NewRequest(http.MethodPost, "https://app.example.com/x/y/z", nil)
			rq.Header.Set("Authorization", "Bearer "+token)
			rt.ServeHTTP(rr, rq)

			if !assert.Equal(t, http.StatusOK, rr.Code) {
				t.Log(rr.Body.String())
			}
		})
		t.Run("no-no for another", func(t *testing.T) {
			rr := httptest.NewRecorder()
			rq := httptest.NewRequest(http.MethodGet, "https://app.example.com/x/y/z", nil)
			rq.Header.Set("Authorization", "Bearer "+token)
			rt.ServeHTTP(rr, rq)

			assert.Equal(t, http.StatusForbidden, rr.Code)
		})
	})
}
