package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/client"
	"github.com/hashicorp/go-multierror"
	"github.com/reddec/git-pipe/backup"
	"github.com/reddec/git-pipe/backup/filebackup"
	"github.com/reddec/git-pipe/backup/nobackup"
	"github.com/reddec/git-pipe/backup/objectstore"
	"github.com/reddec/git-pipe/core"
	"github.com/reddec/git-pipe/core/dns/cf"
	"github.com/reddec/git-pipe/core/dns/noregister"
	"github.com/reddec/git-pipe/core/dns/singlehost"
	"github.com/reddec/git-pipe/core/event"
	"github.com/reddec/git-pipe/core/ingress"
	"github.com/reddec/git-pipe/core/ingress/dummy"
	"github.com/reddec/git-pipe/core/ingress/embedded"
	"github.com/reddec/git-pipe/core/network"
	"github.com/reddec/git-pipe/core/storage"
	"github.com/reddec/git-pipe/cryptor/symmetric"
	"github.com/reddec/git-pipe/internal"
	"github.com/reddec/git-pipe/pipe"
	"github.com/reddec/git-pipe/remote"
	"github.com/reddec/git-pipe/remote/git"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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
	LogMode          string        `long:"log-mode" env:"LOG_MODE" description:"Logger mode" default:"development" choice:"production" choice:"development"`
	LogLevel         logLevel      `long:"log-level" env:"LOG_LEVEL" description:"Log level" default:"debug"`
	Provider         string        `long:"provider" short:"p" env:"PROVIDER" description:"DNS provider for auto registration" choice:"cloudflare"`
	Cloudflare       cf.Config     `group:"Cloudflare config" namespace:"cloudflare" env-namespace:"CLOUDFLARE"`

	Args struct {
		Repos []string `positional-arg-name:"git-url" required:"1" description:"remote git URL to poll with optional branch/tag name after hash"`
	} `positional-args:"true"`
}

type Router struct {
	Domain      string `long:"domain" short:"d" env:"DOMAIN" default:"localhost" description:"Root domain, default is hostname"`
	Dummy       bool   `long:"dummy" short:"D" env:"DUMMY" description:"Dummy mode disables HTTP router"`
	Bind        string `long:"bind" short:"b" env:"BIND" description:"Addresses to where bind HTTP server" default:"127.0.0.1:8080"`
	AutoTLS     bool   `long:"auto-tls" short:"T" env:"AUTO_TLS" description:"Automatic TLS (Let's Encrypt), ignores bind address and uses 0.0.0.0:443 port"`
	TLS         bool   `long:"tls" env:"TLS" description:"Enable HTTPS serving with TLS. TLS files should support multiple domains, otherwise path-routing should be enabled. Ignored with --auto-tls'" json:"tls"`
	SSLDir      string `long:"ssl-dir" env:"SSL_DIR" description:"Directory for SSL certificates and keys. Should contain server.{crt,key} files unless auto-tls enabled. For auto-tls it is used as cache dir" default:"ssl"`
	NoIndex     bool   `long:"no-index" env:"NO_INDEX" description:"Disable index page"`
	PathRouting bool   `long:"path-routing" short:"P" env:"PATH_ROUTING" description:"Enable path routing instead of domain-based. Implicitly disables --domain"`
	JWT         string `long:"jwt" env:"JWT" description:"Define JWT secret and enable JWT-based authorization"`
}

func (cmd *CommandRun) Execute([]string) error {
	if cmd.Router.Domain == "" {
		name, err := os.Hostname()
		if err != nil {
			return fmt.Errorf("detect hostname: %w", err)
		}
		cmd.Router.Domain = name
	}

	logger, err := cmd.logger()
	if err != nil {
		return fmt.Errorf("create logger: %w", err)
	}
	zap.ReplaceGlobals(logger)
	defer logger.Sync()

	ctx, cancel := context.WithCancel(global)
	defer cancel()

	backupProvider, err := cmd.createBackupProvider()
	if err != nil {
		return fmt.Errorf("initialize storage: %w", err)
	}

	encryption := &symmetric.Symmetric{Key: cmd.BackupKey}

	dnsProvider, err := cmd.createDNSProvider(ctx)
	if err != nil {
		return fmt.Errorf("create DNS: %w", err)
	}

	if cmd.Router.PathRouting {
		dnsProvider = singlehost.New(cmd.Router.Domain, dnsProvider)
	}

	docker, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("create docker client: %w", err)
	}
	defer docker.Close()

	dockerNetwork, err := network.NewDockerNetwork(ctx, docker, cmd.Network)
	if err != nil {
		return fmt.Errorf("create docker network: %w", err)
	}

	var (
		ingressImpl core.Ingress
		router      *embedded.Router
	)
	if cmd.Router.Dummy {
		ingressImpl = dummy.New()
	} else {
		var resolver embedded.RequestResolver
		if cmd.Router.PathRouting {
			resolver = embedded.ByPath()
		} else {
			resolver = embedded.ByDomain(cmd.Router.Domain)
		}

		var chain []embedded.RouteHandler
		if cmd.Router.JWT != "" {
			chain = append(chain, embedded.JWT(cmd.Router.JWT))
		}
		chain = append(chain, embedded.Proxy(dockerNetwork))
		router = embedded.New(resolver, chain...)
		router.Index(!cmd.Router.NoIndex)
		ingressImpl = ingress.New(router)
	}

	env := core.Base{
		DNS:     dnsProvider,
		Ingress: ingressImpl,
		Backup:  storage.New(backupProvider, docker, encryption, "", "local", cmd.BackupInterval),
		Network: dockerNetwork,
		Docker:  docker,
	}

	var wg multierror.Group

	if router != nil {
		wg.Go(func() error {
			defer cancel()
			return cmd.runRouter(ctx, router)
		})
	}

	environ, err := cmd.environment()
	if err != nil {
		return fmt.Errorf("read environment: %w", err)
	}

	for _, repo := range cmd.Args.Repos {
		source, err := git.FromURL(repo)
		if err != nil {
			return fmt.Errorf("load repo %s: %w", repo, err)
		}

		name := cmd.repoName(source)

		dir := filepath.Join(cmd.Output, name)
		repoEnv := &core.Environment{
			Base:      env,
			Name:      name,
			Directory: dir,
			Vars:      filterEnvironment(environ, name),
			Event:     event.Noop(),
		}

		ref := source.Ref()
		logger.Info("repository detected", zap.String("repo", ref.Redacted()), zap.String("name", name), zap.String("workdir", dir))

		wg.Go(func() error {
			defer cancel()
			pipe.Run(ctx, source, repoEnv, cmd.Interval)
			return nil
		})
	}

	return wg.Wait().ErrorOrNil()
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

func (cmd CommandRun) createDNSProvider(ctx context.Context) (core.DNS, error) {
	switch cmd.Provider {
	case "cloudflare":
		p, err := cf.New(ctx, cmd.Cloudflare)
		if err != nil {
			return nil, fmt.Errorf("create cloudflare DNS provider: %w", err)
		}
		return p, nil
	case "":
		return &noregister.NoRegister{}, nil
	default:
		return nil, errUnknownProvider
	}
}

func (cmd CommandRun) runRouter(ctx context.Context, router *embedded.Router) error {
	var allowedDomains = embedded.Static(cmd.Router.Domain)
	if !cmd.Router.PathRouting {
		allowedDomains = router
	}

	switch {
	case cmd.Router.AutoTLS:
		return embedded.RunAutoTLS(ctx, cmd.Router.SSLDir, allowedDomains, router)
	case cmd.Router.TLS:
		return embedded.RunTLS(ctx, cmd.Router.Bind, cmd.Router.SSLDir, router)
	default:
		return embedded.Run(ctx, cmd.Router.Bind, router)
	}
}

func (cmd CommandRun) repoName(source remote.Source) string {
	ref := source.Ref()
	if cmd.FQDN {
		return generateFullName(ref)
	}
	return generateSimpleName(ref)
}

func (cmd CommandRun) environment() (map[string]string, error) {
	var env = make(map[string]string)
	for _, file := range cmd.EnvFile {
		v, err := internal.ReadEnvFile(file)
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

func (cmd CommandRun) logger() (*zap.Logger, error) {
	opt := zap.IncreaseLevel(zap.NewAtomicLevelAt(zapcore.Level(cmd.LogLevel)))
	switch cmd.LogMode {
	case "production":
		return zap.NewProduction(opt)
	default:
		return zap.NewDevelopment(opt)
	}
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

func generateSimpleName(u url.URL) string {
	names := strings.Split(u.Path, "/")
	name := names[len(names)-1]
	name = strings.TrimSuffix(name, ".git")
	name = internal.ToDomain(name)
	if name == "" {
		return generateFullName(u)
	}
	return name
}

func generateFullName(u url.URL) string {
	baseName := u.Path

	name := strings.ReplaceAll(baseName, "/", ".")
	name = strings.Trim(name, ".git")
	name = internal.ToDomain(name)
	name = strings.Trim(name, ".")

	parts := strings.Split(name, ".")
	for i := 0; i < len(parts)/2; i++ {
		parts[i], parts[len(parts)-i-1] = parts[len(parts)-i-1], parts[i]
	}

	domain := strings.Join(parts, ".")

	if hostname := u.Hostname(); hostname != "" {
		domain += "." + hostname
	}
	return domain
}

type logLevel zapcore.Level

func (ll *logLevel) UnmarshalFlag(value string) error {
	return (*zapcore.Level)(ll).UnmarshalText([]byte(value))
}
