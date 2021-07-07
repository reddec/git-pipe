package pipe

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/reddec/git-pipe/backup"
	"github.com/reddec/git-pipe/backup/nobackup"
	"github.com/reddec/git-pipe/cryptor"
	"github.com/reddec/git-pipe/cryptor/noecnryption"
	"github.com/reddec/git-pipe/dns"
	"github.com/reddec/git-pipe/dns/noregister"
	"github.com/reddec/git-pipe/internal"
	"github.com/reddec/git-pipe/packs"
	"github.com/reddec/git-pipe/remote"
	"github.com/reddec/git-pipe/router"
	"github.com/reddec/git-pipe/router/dummy"
)

const (
	defaultPermission = 0700
)

var (
	errUnknownPackage = errors.New("unknown packaging for repo")
)

func Default(ctx context.Context) (*Manager, error) {
	return New(ctx, Config{
		Network:   "git-pipe",
		Directory: "./git-pipe",
		FQDN:      false,
		Poll:      time.Minute,
		Shutdown:  time.Minute,
		Backup:    time.Hour,
	})
}

func New(ctx context.Context, cfg Config) (*Manager, error) {
	const (
		defaultEventBuffer = 1024
		defaultBackupTime  = time.Hour
	)

	if cfg.EventBuffer <= 0 {
		cfg.EventBuffer = defaultEventBuffer
	}

	if cfg.Backup <= 0 {
		cfg.Backup = defaultBackupTime
	}

	logger := internal.SubLogger(ctx, "manager")

	networkID, err := cfg.createNetwork(ctx)
	if err != nil {
		return nil, fmt.Errorf("create network: %w", err)
	}

	containerID := internal.ContainerID()
	if containerID != "" {
		logger.Println("self container ID:", containerID)
		if err := internal.JoinNetwork(ctx, containerID, networkID); err != nil {
			return nil, fmt.Errorf("join self to network: %w", err)
		}
	}

	return &Manager{
		config:   cfg,
		router:   &dummy.Dummy{},
		backup:   &nobackup.NoBackup{},
		cryptor:  &noecnryption.NoEncryption{},
		registry: &noregister.NoRegister{},
		ready:    make(chan remote.Source, cfg.EventBuffer),
		logger:   logger,
		network: packs.Network{
			ID:   networkID,
			Name: cfg.Network,
		},
	}, nil
}

type Config struct {
	Domain      string        // (optional) root domain
	Network     string        // docker network name that will be used for communication. Will be created if not exists
	Directory   string        // directory for cloning repos
	FQDN        bool          // generate project name based on full repo URL, otherwise only last past of path will be used
	Poll        time.Duration // poll interval
	Shutdown    time.Duration // graceful shutdown
	Backup      time.Duration // backup interval
	EventBuffer int           // ready events buffer
}

func (cfg Config) createNetwork(ctx context.Context) (string, error) {
	n, err := internal.CreateNetworkIfNeeded(ctx, cfg.Network)
	if err != nil {
		return "", fmt.Errorf("create network %s: %w", cfg.Network, err)
	}
	return n, nil
}

type Manager struct {
	config   Config
	network  packs.Network
	router   router.Router
	backup   backup.Backup
	registry dns.DNS
	cryptor  cryptor.Cryptor
	logger   internal.Logger
	ready    chan remote.Source
}

// Network for internal communications.
func (mgt *Manager) Network() packs.Network {
	return mgt.network
}

// Router for requests.
func (mgt *Manager) Router(router router.Router) {
	mgt.router = router
}

// Ready events.
func (mgt *Manager) Ready() <-chan remote.Source {
	return mgt.ready
}

// Encrypt backup.
func (mgt *Manager) Encrypt(encryptor cryptor.Cryptor) {
	mgt.cryptor = encryptor
}

// Backup provider.
func (mgt *Manager) Backup(backuper backup.Backup) {
	mgt.backup = backuper
}

// DNS provider.
func (mgt *Manager) DNS(registry dns.DNS) {
	mgt.registry = registry
}

// Logger for manager and pipes.
func (mgt *Manager) Logger(logger internal.Logger) {
	mgt.logger = logger
}

// Name of remote source.
func (mgt *Manager) Name(u url.URL) string {
	if mgt.config.FQDN {
		return generateFullName(u)
	}
	return generateSimpleName(u)
}

// Run pipe for defined repo. Will be block till context canceled.
func (mgt *Manager) Run(ctx context.Context, source remote.Source, env map[string]string) error {
	p, err := mgt.newPipe(source, env)
	if err != nil {
		return fmt.Errorf("create pipe: %w", err)
	}
	return p.run(ctx)
}

func (mgt *Manager) location(u url.URL) string {
	return filepath.Join(mgt.config.Directory, mgt.Name(u))
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
