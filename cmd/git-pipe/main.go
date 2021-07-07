package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/reddec/git-pipe/backup"
	"github.com/reddec/git-pipe/backup/filebackup"
	"github.com/reddec/git-pipe/backup/nobackup"
	"github.com/reddec/git-pipe/backup/objectstore"
	"github.com/reddec/git-pipe/cryptor/symmetric"
	"github.com/reddec/git-pipe/dns"
	"github.com/reddec/git-pipe/dns/cf"
	"github.com/reddec/git-pipe/pipe"
	"github.com/reddec/git-pipe/remote/git"
	"github.com/reddec/git-pipe/router"
	"github.com/reddec/git-pipe/router/embedded"

	"github.com/hashicorp/go-multierror"
	"github.com/jessevdk/go-flags"
	"golang.org/x/crypto/acme/autocert"
)

const version = "dev"

type Config struct {
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
	Domain  string `long:"domain" short:"d" env:"DOMAIN" default:"localhost" description:"Root domain, default is hostname"`
	Dummy   bool   `long:"dummy" short:"D" env:"DUMMY" description:"Dummy mode disables HTTP router"`
	Bind    string `long:"bind" short:"b" env:"BIND" description:"Address to where bind HTTP server" default:"127.0.0.1:8080"`
	AutoTLS bool   `long:"auto-tls" short:"T" env:"AUTO_TLS" description:"Automatic TLS (Let's Encrypt), ignores bind address and uses 0.0.0.0:443 port"`
	TLS     bool   `long:"tls" env:"TLS" description:"Enable HTTPS serving with TLS. TLS files should support multiple domains, otherwise path-routing should be enabled. Ignored with --auto-tls'" json:"tls"`
	SSLDir  string `long:"ssl-dir" env:"SSL_DIR" description:"Directory for SSL certificates and keys. Should contain server.{crt,key} files unless auto-tls enabled. For auto-tls it is used as cache dir" default:"ssl"`
	NoIndex bool   `long:"no-index" env:"NO_INDEX" description:"Disable index page"`
}

func main() {
	var config Config
	parser := flags.NewParser(&config, flags.Default)
	parser.LongDescription = "Watch and deploy docker-based applications from Git\nAuthor: Baryshnikov Aleksandr <dev@baryshnikov.net>\nVersion: " + version
	_, err := parser.Parse()
	if err != nil {
		os.Exit(1)
	}

	if config.Router.Domain == "" {
		name, err := os.Hostname()
		if err != nil {
			log.Panic("detect hostname:", err)
		}
		config.Router.Domain = name
	}

	ctx, closer := signal.NotifyContext(context.Background(), os.Interrupt)
	defer closer()
	err = run(ctx, config)

	if err != nil {
		log.Panic(err)
	}
}

func run(global context.Context, config Config) error {
	manager, err := pipe.New(global, pipe.Config{
		Network:   config.Network,
		Directory: config.Output,
		FQDN:      config.FQDN,
		Poll:      config.Interval,
		Shutdown:  config.GracefulShutdown,
		Backup:    config.BackupInterval,
		Domain:    config.Router.Domain,
	})

	if err != nil {
		return fmt.Errorf("create manager: %w", err)
	}

	ctx, cancel := context.WithCancel(global)
	defer cancel()

	if err := config.addBackup(manager); err != nil {
		return fmt.Errorf("setup backup: %w", err)
	}

	if err := config.addDNSProvider(ctx, manager); err != nil {
		return fmt.Errorf("setup DNS provider: %w", err)
	}

	env, err := config.environment()
	if err != nil {
		return fmt.Errorf("read environment: %w", err)
	}

	manager.Encrypt(&symmetric.Symmetric{Key: config.BackupKey})

	var wg multierror.Group

	if !config.Router.Dummy {
		wg.Go(func() error {
			defer cancel()
			if err := config.runRouter(ctx, manager); err == nil || errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		})
	}

	for _, repo := range config.Args.Repos {
		source, err := git.FromURL(repo)
		if err != nil {
			return fmt.Errorf("load repo %s: %w", repo, err)
		}

		wg.Go(func() error {
			defer cancel()
			err := manager.Run(ctx, source, filterEnvironment(env, manager.Name(source.Ref())))
			if err != nil {
				return fmt.Errorf("run manager: %w", err)
			}
			return nil
		})
	}

	if err := wg.Wait().ErrorOrNil(); err != nil {
		return fmt.Errorf("run: %w", err)
	}
	return nil
}

func (cfg Config) addBackup(manager *pipe.Manager) error {
	backuper, err := cfg.backupProvider()
	if err != nil {
		return fmt.Errorf("create backup provider: %w", err)
	}

	manager.Backup(backuper)
	return nil
}

func (cfg Config) addDNSProvider(ctx context.Context, manager *pipe.Manager) error {
	if cfg.Provider == "" {
		return nil
	}
	p, err := cfg.createDNSProvider(ctx)
	if err != nil {
		return fmt.Errorf("create provider: %w", err)
	}
	manager.DNS(p)
	return nil
}

func (cfg Config) runRouter(ctx context.Context, manager *pipe.Manager) error {
	port, err := cfg.port()
	if err != nil {
		return fmt.Errorf("get port: %w", err)
	}
	proxy := embedded.New(embedded.Config{
		Index: !cfg.Router.NoIndex,
		Port:  port,
	})
	stated := router.WithState(proxy)
	manager.Router(stated)

	var listener net.Listener
	if cfg.Router.AutoTLS {
		listener = cfg.createAutoTLSListener(stated)
		cfg.Router.TLS = false
	} else {
		listener, err = net.Listen("tcp", cfg.Router.Bind)
		if err != nil {
			return fmt.Errorf("create listener: %w", err)
		}
	}
	defer listener.Close()

	srv := &http.Server{Addr: cfg.Router.Bind, Handler: proxy}

	child, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		<-child.Done()
		_ = srv.Close()
	}()
	if cfg.Router.TLS {
		return srv.ServeTLS(listener, filepath.Join(cfg.Router.SSLDir, "server.crt"), filepath.Join(cfg.Router.SSLDir, "server.key"))
	}
	return srv.Serve(listener)
}

var (
	errUnknownProvider       = errors.New("unknown provider")
	errUnknownBackupProtocol = errors.New("unknown backup protocol")
)

func (cfg Config) backupProvider() (backup.Backup, error) {
	if cfg.Backup == "" || cfg.Backup == "none" {
		return &nobackup.NoBackup{}, nil
	}
	u, err := url.Parse(cfg.Backup)
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

func (cfg Config) createDNSProvider(ctx context.Context) (dns.DNS, error) {
	switch cfg.Provider {
	case "cloudflare":
		p, err := cf.New(ctx, cfg.Cloudflare)
		if err != nil {
			return nil, fmt.Errorf("create cloudflare DNS provider: %w", err)
		}
		return p, nil
	default:
		return nil, errUnknownProvider
	}
}

func (cfg Config) port() (int, error) {
	const TLSPort = 443

	if cfg.Router.AutoTLS {
		return TLSPort, nil
	}
	_, port, err := net.SplitHostPort(cfg.Router.Bind)
	if err != nil {
		return 0, fmt.Errorf("split binding address to host and port: %w", err)
	}
	value, err := strconv.Atoi(port)
	if err != nil {
		return 0, fmt.Errorf("parse port: %w", err)
	}
	return value, nil
}

var errUnknownDomain = errors.New("unknown domain")

func (cfg Config) createAutoTLSListener(domainsProvider interface {
	HasDomain(domain string) bool
}) net.Listener {
	manager := &autocert.Manager{
		Prompt: autocert.AcceptTOS,
		Cache:  autocert.DirCache(cfg.Router.SSLDir),
		HostPolicy: func(ctx context.Context, host string) error {
			if domainsProvider.HasDomain(host) {
				return nil
			}
			return errUnknownDomain
		},
	}

	return manager.Listener()
}

func (cfg Config) environment() (map[string]string, error) {
	var env = make(map[string]string)
	for _, file := range cfg.EnvFile {
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
