package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/reddec/git-pipe/backup"
	"github.com/reddec/git-pipe/backup/filebackup"
	"github.com/reddec/git-pipe/backup/nobackup"
	"github.com/reddec/git-pipe/backup/objectstore"
	"github.com/reddec/git-pipe/core"
	v1 "github.com/reddec/git-pipe/core/v1"
	"github.com/reddec/git-pipe/cryptor/symmetric"
	"github.com/reddec/git-pipe/dns"
	"github.com/reddec/git-pipe/dns/cf"
	"github.com/reddec/git-pipe/pipe"
	"github.com/reddec/git-pipe/remote/git"
	"github.com/reddec/git-pipe/router"
	"golang.org/x/crypto/acme/autocert"
)

type CommandRun struct {
	Router           Router
	Network          string        `long:"network" short:"n" env:"NETWORK" description:"Network name for internal communication" default:"git-pipe"`
	Interval         time.Duration `long:"interval" short:"i" env:"INTERVAL" description:"Interval to poll repositories" default:"30s"`
	Output           string        `long:"output" short:"o" env:"OUTPUT" description:"Output directory for clone" default:"repos"`
	Backup           string        `long:"backup" short:"B" env:"BACKUP" description:"Backup location" default:"file://backups"`
	BackupKey        string        `long:"backup-key" short:"K" env:"BACKUP_KEY" description:"Backup key" default:"git-pipe-change-me"`
	BackupInterval   time.Duration `long:"backup-interval" short:"I" env:"BACKUP_INTERVAL" description:"Backup interval" default:"1h"`
	FQDN             bool          `long:"fqdn" short:"F" env:"FQDN" description:"Construct from URL unique FQDN based on path and domain"`
	GracefulShutdown time.Duration `long:"graceful-shutdown" env:"GRACEFUL_SHUTDOWN" description:"Interval before server shutdown" default:"15s"`
	EnvFile          []string      `long:"env-file" short:"e" env:"ENV_FILE" description:"Environment variables files"`

	Provider   string    `long:"provider" short:"p" env:"PROVIDER" description:"DNS provider for auto registration" choice:"cloudflare"`
	Cloudflare cf.Config `group:"Cloudflare config" namespace:"cloudflare" env-namespace:"CLOUDFLARE"`

	Args struct {
		Repos []string `positional-arg-name:"git-url" required:"1" description:"remote git URL to poll with optional branch/tag name after hash"`
	} `positional-args:"true"`
}

type Router struct {
	Domain      string `long:"domain" short:"d" env:"DOMAIN" default:"localhost" description:"Root domain, default is hostname"`
	Dummy       bool   `long:"dummy" short:"D" env:"DUMMY" description:"Dummy mode disables HTTP router"`
	Bind        string `long:"bind" short:"b" env:"BIND" description:"Address to where bind HTTP server" default:"127.0.0.1:8080"`
	AutoTLS     bool   `long:"auto-tls" short:"T" env:"AUTO_TLS" description:"Automatic TLS (Let's Encrypt), ignores bind address and uses 0.0.0.0:443 port"`
	TLS         bool   `long:"tls" env:"TLS" description:"Enable HTTPS serving with TLS. TLS files should support multiple domains, otherwise path-routing should be enabled. Ignored with --auto-tls'" json:"tls"`
	SSLDir      string `long:"ssl-dir" env:"SSL_DIR" description:"Directory for SSL certificates and keys. Should contain server.{crt,key} files unless auto-tls enabled. For auto-tls it is used as cache dir" default:"ssl"`
	NoIndex     bool   `long:"no-index" env:"NO_INDEX" description:"Disable index page"`
	PathRouting bool   `long:"path-routing" short:"P" env:"PATH_ROUTING" description:"Enable path routing instead of domain-based. Implicitly disables --domain"`
	JWT         string `long:"jwt" env:"JWT" description:"Define JWT secret and enable JWT-based authorization"`
}

func (rt Router) domain() string {
	if rt.PathRouting {
		return ""
	}
	return rt.Domain
}

func (cmd *CommandRun) Execute([]string) error {
	if cmd.Router.Domain == "" {
		name, err := os.Hostname()
		if err != nil {
			return fmt.Errorf("detect hostname: %w", err)
		}
		cmd.Router.Domain = name
	}

	ctx, cancel := context.WithCancel(global)
	defer cancel()

	storage, err := cmd.createBackupProvider()
	if err != nil {
		return fmt.Errorf("initialize storage: %w", err)
	}

	encryption := &symmetric.Symmetric{Key: cmd.BackupKey}

	cfg := v1.DefaultConfig()
	cfg.Domain = cmd.Router.Domain
	cfg.NetworkName = cmd.Network
	cfg.GracefulTimeout = cmd.GracefulShutdown
	cfg.RetryDeployInterval = cmd.GracefulShutdown

	env, err := v1.NewBackground(ctx, cfg, storage, encryption)
	if err != nil {
		return fmt.Errorf("create env: %w", err)
	}

	defer env.Stop()

	if cmd.Provider != "" {
		if d, err := cmd.createDNSProvider(ctx); err != nil {
			return fmt.Errorf("create DNS: %w", err)
		} else if err := env.Launcher().Launch(ctx, core.Descriptor{
			Name:   "@dns-provider",
			Daemon: dns.Daemonize(d),
		}); err != nil {
			return fmt.Errorf("launch DNS provider: %w", err)
		}
	}

	environ, err := cmd.environment()
	if err != nil {
		return fmt.Errorf("read environment: %w", err)
	}

	if !cmd.Router.Dummy {
		var handlers []router.RouteHandler
		if cmd.Router.JWT != "" {
			handlers = append(handlers, router.JWT(cmd.Router.JWT))
		}

		_ = env.Launcher().Launch(ctx, core.Descriptor{
			Name: "@router",
			Daemon: router.New(router.Config{
				Bind:        cmd.Router.Bind,
				AutoTLS:     cmd.Router.AutoTLS,
				TLS:         cmd.Router.TLS,
				SSLDir:      cmd.Router.SSLDir,
				NoIndex:     cmd.Router.NoIndex,
				PathRouting: cmd.Router.PathRouting,
			}),
		})
	}

	for _, repo := range cmd.Args.Repos {
		source, err := git.FromURL(repo)
		if err != nil {
			return fmt.Errorf("load repo %s: %w", repo, err)
		}

		name := pipe.Name(source.Ref(), cmd.FQDN)
		d := pipe.Poller(source, cmd.Interval, cmd.BackupInterval, cmd.FQDN, cmd.Output, filterEnvironment(environ, name))
		err = env.Launcher().Launch(ctx, core.Descriptor{
			Name:   name,
			Daemon: d,
		})
		if err != nil {
			return fmt.Errorf("launch %s: %w", name, err)
		}
	}

	env.Wait()
	return nil
}

var (
	errUnknownProvider       = errors.New("unknown provider")
	errUnknownBackupProtocol = errors.New("unknown backup protocol")
)

func (cmd CommandRun) createBackupProvider() (backup.Backup, error) {
	if cmd.Backup == "" || cmd.Backup == "none" {
		return &nobackup.NoBackup{}, nil
	}
	u, err := url.Parse(cmd.Backup)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	switch u.Scheme {
	case "s3":
		return objectstore.FromURL(*u), nil
	case "", "file", "dir":
		return &filebackup.FileBackup{Directory: filepath.Join(u.Host, u.Path)}, nil
	default:
		return nil, errUnknownBackupProtocol
	}
}

func (cmd CommandRun) createDNSProvider(ctx context.Context) (dns.DNS, error) {
	switch cmd.Provider {
	case "cloudflare":
		p, err := cf.New(ctx, cmd.Cloudflare)
		if err != nil {
			return nil, fmt.Errorf("create cloudflare DNS provider: %w", err)
		}
		return p, nil
	default:
		return nil, errUnknownProvider
	}
}

var errUnknownDomain = errors.New("unknown domain")

func (cmd CommandRun) createAutoTLSListener(domainsProvider interface {
	HasDomain(domain string) bool
}) net.Listener {
	manager := &autocert.Manager{
		Prompt: autocert.AcceptTOS,
		Cache:  autocert.DirCache(cmd.Router.SSLDir),
		HostPolicy: func(ctx context.Context, host string) error {
			if (cmd.Router.Domain == host && cmd.Router.PathRouting) || domainsProvider.HasDomain(host) {
				return nil
			}
			return errUnknownDomain
		},
	}

	return manager.Listener()
}

func (cmd CommandRun) environment() (map[string]string, error) {
	var env = make(map[string]string)
	for _, file := range cmd.EnvFile {
		v, err := readEnvFile(file)
		if err != nil {
			return nil, fmt.Errorf("read env file: %w", err)
		}
		for k, v := range v {
			env[k] = v
		}
	}
	// merge system env
	for _, item := range os.Environ() {
		kv := strings.SplitN(item, "=", 2) //nolint:gomnd
		if len(kv) != 0 {
			continue
		}
		key, value := kv[0], kv[1]
		env[key] = value
	}

	return env, nil
}

func filterEnvironment(environ map[string]string, repoName string) map[string]string {
	appPrefix := strings.ReplaceAll(strings.ToUpper(repoName), "-", "_") + "_"

	var res = map[string]string{}
	for key, value := range environ {
		if !strings.HasPrefix(key, appPrefix) {
			continue
		}
		realKey := strings.TrimPrefix(key, appPrefix)
		res[realKey] = value
	}
	return res
}

func readEnvFile(filename string) (map[string]string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	ans := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}

		kv := strings.SplitN(line, "=", 2) //nolint:gomnd
		ans[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
	}

	return ans, scanner.Err()
}
