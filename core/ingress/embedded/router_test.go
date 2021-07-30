package embedded_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/dgrijalva/jwt-go"
	"github.com/reddec/git-pipe/core/ingress"
	"github.com/reddec/git-pipe/core/ingress/embedded"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRouter_ServeHTTP(t *testing.T) {
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	zap.ReplaceGlobals(logger)
	defer logger.Sync()

	t.Run("basic domain routing", func(t *testing.T) {
		ctx := context.Background()
		var routed bool
		rt := embedded.New(embedded.ByRoot(), embedded.RouteHandlerFunc(func(writer http.ResponseWriter, request *http.Request, record embedded.Route) error {
			routed = true
			writer.WriteHeader(http.StatusOK)
			return nil
		}))

		err := rt.Set(ctx, []ingress.Record{
			{
				Domain: "app.example.com",
				Group:  "app",
			},
		})
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		rq := httptest.NewRequest(http.MethodGet, "https://app.example.com/x/y/z", nil)
		rq.Header.Set("Host", "app.example.com")
		rt.ServeHTTP(rr, rq)
		assert.True(t, routed)
		assert.Equal(t, http.StatusOK, rr.Code)
	})
	t.Run("unknown domain returns 404", func(t *testing.T) {
		ctx := context.Background()
		var routed bool
		rt := embedded.New(embedded.ByRoot(), embedded.RouteHandlerFunc(func(writer http.ResponseWriter, request *http.Request, record embedded.Route) error {
			routed = true
			writer.WriteHeader(http.StatusOK)
			return nil
		}))
		err := rt.Set(ctx, []ingress.Record{
			{
				Domain: "app.example.com",
				Group:  "app",
			},
		})
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		rq := httptest.NewRequest(http.MethodGet, "https://app1.example.com/x/y/z", nil)
		rt.ServeHTTP(rr, rq)
		assert.False(t, routed)
		assert.Equal(t, http.StatusNotFound, rr.Code)
	})
	t.Run("basic path routing", func(t *testing.T) {
		ctx := context.Background()
		var routed bool
		rt := embedded.New(embedded.ByPath(), embedded.RouteHandlerFunc(func(writer http.ResponseWriter, request *http.Request, record embedded.Route) error {
			routed = true
			writer.WriteHeader(http.StatusOK)
			return nil
		}))
		err := rt.Set(ctx, []ingress.Record{
			{
				Domain: "app.example.com",
				Group:  "app",
			},
		})
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		rq := httptest.NewRequest(http.MethodGet, "https://nomatter.example.com/app.example.com/some/path", nil)
		rt.ServeHTTP(rr, rq)
		assert.True(t, routed)
		assert.Equal(t, http.StatusOK, rr.Code)
	})
	t.Run("path routing should trim source prefix", func(t *testing.T) {
		ctx := context.Background()
		var routed bool
		rt := embedded.New(embedded.ByPath(), embedded.RouteHandlerFunc(func(writer http.ResponseWriter, request *http.Request, record embedded.Route) error {
			routed = true
			assert.Equal(t, "/some/path", request.URL.Path)
			writer.WriteHeader(http.StatusOK)
			return nil
		}))
		err := rt.Set(ctx, []ingress.Record{
			{
				Domain: "app.example.com",
				Group:  "app",
			},
		})
		require.NoError(t, err)
		rr := httptest.NewRecorder()
		rq := httptest.NewRequest(http.MethodGet, "https://nomatter.example.com/app.example.com/some/path", nil)
		rt.ServeHTTP(rr, rq)
		assert.True(t, routed)
		assert.Equal(t, http.StatusOK, rr.Code)
	})
	t.Run("basic sub-domain routing", func(t *testing.T) {
		ctx := context.Background()
		var routed bool
		rt := embedded.New(embedded.ByDomain("example.com"), embedded.RouteHandlerFunc(func(writer http.ResponseWriter, request *http.Request, record embedded.Route) error {
			routed = true
			writer.WriteHeader(http.StatusOK)
			return nil
		}))

		err := rt.Set(ctx, []ingress.Record{
			{
				Domain: "myapp.service",
				Group:  "app",
			},
		})
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		rq := httptest.NewRequest(http.MethodGet, "https://myapp.service.example.com/x/y/z", nil)
		rt.ServeHTTP(rr, rq)
		assert.True(t, routed)
		assert.Equal(t, http.StatusOK, rr.Code)
	})
}

func TestRequest(t *testing.T) {
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	zap.ReplaceGlobals(logger)
	defer logger.Sync()

	var routed atomic.Value
	routed.Store(false)

	srv := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		routed.Store(true)
		assert.Equal(t, "/x/y/z", request.URL.Path)
	}))
	defer srv.Close()

	rt := embedded.New(embedded.ByRoot(), embedded.Proxy(nil))

	ctx := context.Background()

	err = rt.Set(ctx, []ingress.Record{
		{
			Domain:    "app.example.com",
			Group:     "app",
			Addresses: []string{srv.Listener.Addr().String()},
		},
	})
	require.NoError(t, err)
	rr := httptest.NewRecorder()
	rq := httptest.NewRequest(http.MethodGet, "https://app.example.com/x/y/z", nil)
	rt.ServeHTTP(rr, rq)
	assert.True(t, routed.Load().(bool))
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestJWT(t *testing.T) {
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	zap.ReplaceGlobals(logger)
	defer logger.Sync()

	t.Run("valid token should work", func(t *testing.T) {
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"aud": "client1"}).SignedString([]byte("qwerty"))
		require.NoError(t, err)
		assert.NotEmpty(t, token)

		ctx := context.Background()
		rt := embedded.New(embedded.ByRoot(), embedded.JWT("qwerty"), embedded.RouteHandlerFunc(func(writer http.ResponseWriter, request *http.Request, record embedded.Route) error {
			assert.Equal(t, "client1", request.Header.Get(embedded.HeaderUser))
			writer.WriteHeader(http.StatusOK)
			return nil
		}))
		err = rt.Set(ctx, []ingress.Record{
			{
				Domain: "app.example.com",
				Group:  "app",
			},
		})
		require.NoError(t, err)

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

		ctx := context.Background()
		rt := embedded.New(embedded.ByRoot(), embedded.JWT("qwerty"), embedded.RouteHandlerFunc(func(writer http.ResponseWriter, request *http.Request, record embedded.Route) error {
			writer.WriteHeader(http.StatusOK)
			return nil
		}))
		err = rt.Set(ctx, []ingress.Record{
			{
				Domain: "app.example.com",
				Group:  "my-repo",
			},
			{
				Domain: "app2.example.com",
				Group:  "my-repo-2",
			},
		})
		require.NoError(t, err)

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

		ctx := context.Background()
		rt := embedded.New(embedded.ByRoot(), embedded.JWT("qwerty"), embedded.RouteHandlerFunc(func(writer http.ResponseWriter, request *http.Request, record embedded.Route) error {
			assert.Equal(t, "client1", request.Header.Get(embedded.HeaderUser))
			writer.WriteHeader(http.StatusOK)
			return nil
		}))
		err = rt.Set(ctx, []ingress.Record{
			{
				Domain: "app.example.com",
				Group:  "app",
			},
		})
		require.NoError(t, err)

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
