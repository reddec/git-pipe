package embedded

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"

	"github.com/reddec/git-pipe/internal"
	"go.uber.org/zap"
	"golang.org/x/crypto/acme/autocert"
)

// Run plain HTTP server. Closed automatically in case parent context cancelled.
func Run(global context.Context, bind string, handler http.Handler) error {
	ctx, cancel := context.WithCancel(global)
	defer cancel()
	server := &http.Server{
		Addr:    bind,
		Handler: handler,
	}

	go func() {
		<-ctx.Done()
		if err := server.Close(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			internal.LoggerFromContext(global).Warn("close failed", zap.Error(err))
		}
	}()
	err := server.ListenAndServe()
	if !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// RunTLS starts HTTPS server. Closed automatically in case parent context cancelled.
// In certs dir must be files server.crt and server.key
func RunTLS(global context.Context, bind string, certsDir string, handler http.Handler) error {
	ctx, cancel := context.WithCancel(global)
	defer cancel()
	server := &http.Server{
		Addr:    bind,
		Handler: handler,
	}

	keyFile := filepath.Join(certsDir, "server.key")
	crtFile := filepath.Join(certsDir, "server.crt")

	go func() {
		<-ctx.Done()
		if err := server.Close(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			internal.LoggerFromContext(global).Warn("close failed", zap.Error(err))
		}
	}()
	err := server.ListenAndServeTLS(crtFile, keyFile)
	if !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Domains repository.
type Domains interface {
	Domains() map[string]bool
}

// Static domain set.
func Static(domains ...string) Domains {
	var ans = make(map[string]bool)
	for _, d := range domains {
		ans[d] = true
	}
	return staticDomains(ans)
}

type staticDomains map[string]bool

func (sd staticDomains) Domains() map[string]bool {
	return sd
}

// RunAutoTLS starts HTTPS server on :443 address, requests automatically TLS certificates from Let's encrypt on-demand,
// and stores obtained files in certsDir. Only domains from allowed will be requested.
// Closed automatically in case parent context cancelled.
func RunAutoTLS(global context.Context, certsDir string, allowed Domains, handler http.Handler) error {
	ctx, cancel := context.WithCancel(global)
	defer cancel()

	manager := &autocert.Manager{
		Prompt: autocert.AcceptTOS,
		Cache:  autocert.DirCache(certsDir),
		HostPolicy: func(ctx context.Context, host string) error {
			if allowed.Domains()[host] {
				return nil
			}
			return ErrAbort
		},
	}
	server := &http.Server{
		Handler: handler,
	}
	listener := manager.Listener()

	go func() {
		<-ctx.Done()
		if err := server.Close(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			internal.LoggerFromContext(ctx).Warn("close failed", zap.Error(err))
		}
	}()

	err := server.Serve(listener)
	if !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
