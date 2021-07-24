package pipe

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/reddec/git-pipe/backup"
	"github.com/reddec/git-pipe/cryptor"
	"github.com/reddec/git-pipe/dns"
	"github.com/reddec/git-pipe/internal"
	"github.com/reddec/git-pipe/packs"
	"github.com/reddec/git-pipe/packs/compose"
	"github.com/reddec/git-pipe/remote"
	"github.com/reddec/git-pipe/router"
)

func (mgt *Manager) newPipe(source remote.Source, env map[string]string) (*pipe, error) {
	directory := mgt.location(source.Ref())
	name := filepath.Base(directory)

	logger := internal.Namespaced(mgt.logger, name)

	logger.Println("creating work dir")
	if err := os.MkdirAll(directory, defaultPermission); err != nil {
		return nil, fmt.Errorf("create workdir: %w", err)
	}

	return &pipe{
		directory:        directory,
		name:             filepath.Base(directory),
		logger:           logger,
		restored:         false,
		firstRun:         true,
		forceUpdate:      true,
		source:           source,
		env:              env,
		backupInterval:   mgt.config.Backup,
		pollInterval:     mgt.config.Poll,
		shutdownInterval: mgt.config.Shutdown,
		network:          mgt.network,
		readyCh:          mgt.ready,
		rootDomain:       mgt.config.Domain,

		backupProvider: mgt.backup,
		cryptoProvider: mgt.cryptor,
		dnsProvider:    mgt.registry,
		routerProvider: mgt.router,
	}, nil
}

type pipe struct {
	directory        string
	name             string
	rootDomain       string
	env              map[string]string
	logger           internal.Logger
	restored         bool
	firstRun         bool
	forceUpdate      bool
	current          packs.Pack
	source           remote.Source
	backupInterval   time.Duration
	pollInterval     time.Duration
	shutdownInterval time.Duration
	cryptoProvider   cryptor.Cryptor
	backupProvider   backup.Backup
	dnsProvider      dns.DNS
	routerProvider   *router.router
	readyCh          chan<- remote.Source
	network          packs.Network
}

func (pipe *pipe) run(global context.Context) error {
	ctx := internal.WithLogger(global, pipe.logger)
	backupTicker := time.NewTicker(pipe.backupInterval)
	defer backupTicker.Stop()

	pollerTicker := time.NewTicker(pipe.pollInterval)
	defer pollerTicker.Stop()

	for {
		if next, err := pipe.deploy(ctx); err != nil {
			pipe.logger.Println("run failed:", err)
			pipe.forceUpdate = true
		} else {
			pipe.forceUpdate = false
			pipe.current = next
			pipe.ready()
		}
		select {
		case <-pollerTicker.C:
		case <-backupTicker.C:
			if err := pipe.backup(ctx); err != nil {
				pipe.logger.Println("failed to backup:", err)
			}
		case <-ctx.Done():
			return pipe.shutdown()
		}
	}
}

func (pipe *pipe) deploy(ctx context.Context) (packs.Pack, error) {
	pipe.logger.Println("checking updates...")
	changed, err := pipe.source.Poll(ctx, pipe.directory)
	if err != nil {
		return nil, fmt.Errorf("watch: %w", err)
	}

	if !changed && !pipe.forceUpdate {
		pipe.logger.Println("no updates")
		return nil, nil
	}

	pipe.logger.Println("detecting packaging")
	pack, err := pipe.detectPackage()
	if err != nil {
		return nil, fmt.Errorf("detect package: %w", err)
	}
	pipe.logger.Println("package:", pack)

	pipe.logger.Println("building")
	err = pack.Build(ctx, pipe.env)
	if err != nil {
		return pack, fmt.Errorf("build: %w", err)
	}

	if err := pipe.shutdown(); err != nil {
		return pack, fmt.Errorf("stop previous: %w", err)
	}

	if err := pipe.restore(ctx, pack); err != nil {
		return nil, fmt.Errorf("restore: %w", err)
	}

	pipe.logger.Println("starting new instances")
	services, err := pack.Start(ctx, pipe.env)
	if err != nil {
		return pack, fmt.Errorf("start: %w", err)
	}

	services = pipe.addRootDomain(services)

	pipe.logger.Println("updating domains")
	if err := pipe.dnsProvider.Register(ctx, toDomains(services)); err != nil {
		return nil, fmt.Errorf("update DNS records: %w", err)
	}

	pipe.logger.Println("updating routes")
	pipe.routerProvider.Update(pipe.name, services)

	pipe.logger.Println("updated")
	return pack, nil
}

func (pipe *pipe) addRootDomain(services []packs.Service) []packs.Service {
	if pipe.rootDomain == "" {
		return services
	}
	var ans = make([]packs.Service, len(services))
	for i, v := range services {
		ans[i] = v
		ans[i].Domain += "." + pipe.rootDomain
	}
	return ans
}

func (pipe *pipe) shutdown() error {
	if pipe.current == nil {
		return nil
	}
	pipe.logger.Println("stopping")
	child, cancel := context.WithTimeout(context.Background(), pipe.shutdownInterval)
	defer cancel()
	return pipe.current.Stop(child)
}

func (pipe *pipe) restore(ctx context.Context, pack packs.Pack) error {
	if pipe.restored {
		return nil
	}

	pipe.logger.Println("getting volumes to restore")
	volumes, err := pack.Volumes(ctx)
	if err != nil {
		return fmt.Errorf("list volumes: %w", err)
	}

	if len(volumes) == 0 {
		pipe.logger.Println("no volumes to restore")
		return nil
	}

	pipe.logger.Println("restoring volumes", strings.Join(volumes, ","))
	backupFile, err := ioutil.TempFile("", "")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	err = backupFile.Close()
	if err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	defer os.RemoveAll(backupFile.Name())

	encryptedBackupFile, err := ioutil.TempFile("", "")
	if err != nil {
		return fmt.Errorf("create temp file for encrypted data: %w", err)
	}

	err = encryptedBackupFile.Close()
	if err != nil {
		return fmt.Errorf("close temp file for encrypted data: %w", err)
	}

	pipe.logger.Println("fetching backup archive")
	err = pipe.backupProvider.Restore(ctx, pipe.name, encryptedBackupFile.Name())
	if errors.Is(err, backup.ErrBackupNotExists) {
		// nothing to restore
		return nil
	}
	if err != nil {
		return fmt.Errorf("restore backup: %w", err)
	}

	pipe.logger.Println("decrypting archive")
	err = pipe.cryptoProvider.Decrypt(ctx, encryptedBackupFile.Name(), backupFile.Name())
	if err != nil {
		return fmt.Errorf("decrypt backup: %w", err)
	}

	pipe.logger.Println("unpacking archive")
	err = internal.UnArchiveVolume(ctx, volumes, backupFile.Name())
	if err != nil {
		return fmt.Errorf("copy data to volumes: %w", err)
	}

	pipe.logger.Println("restore complete")

	pipe.restored = true
	return nil
}

func (pipe *pipe) backup(ctx context.Context) error {
	if pipe.current == nil {
		pipe.logger.Println("never run - nothing to backup")
		return nil
	}

	pipe.logger.Println("getting volumes to backup")
	volumes, err := pipe.current.Volumes(ctx)
	if err != nil {
		return fmt.Errorf("list volumes: %w", err)
	}

	if len(volumes) == 0 {
		pipe.logger.Println("nothing to backup")
		return nil
	}

	pipe.logger.Println("backing up volumes")

	backupFile, err := ioutil.TempFile("", "")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	err = backupFile.Close()
	if err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	defer os.RemoveAll(backupFile.Name())

	encryptedBackupFile, err := ioutil.TempFile("", "")
	if err != nil {
		return fmt.Errorf("create temp file for encrypted data: %w", err)
	}
	err = encryptedBackupFile.Close()
	if err != nil {
		return fmt.Errorf("close temp file for encrypted data: %w", err)
	}

	defer os.RemoveAll(encryptedBackupFile.Name())

	pipe.logger.Println("adding data to backup")
	err = internal.ArchiveVolume(ctx, volumes, backupFile.Name())
	if err != nil {
		return fmt.Errorf("archive volumes: %w", err)
	}

	pipe.logger.Println("encrypting backup")
	err = pipe.cryptoProvider.Encrypt(ctx, backupFile.Name(), encryptedBackupFile.Name())
	if err != nil {
		return fmt.Errorf("encrypt archive: %w", err)
	}

	pipe.logger.Println("saving backup")
	err = pipe.backupProvider.Backup(ctx, pipe.name, encryptedBackupFile.Name())
	if err != nil {
		return fmt.Errorf("upload archive: %w", err)
	}

	pipe.logger.Println("done")
	return nil
}

func (pipe *pipe) ready() {
	if !pipe.firstRun {
		return
	}
	pipe.logger.Println("ready")
	select {
	case pipe.readyCh <- pipe.source:
	default:
	}
	pipe.firstRun = false
}

func (pipe *pipe) detectPackage() (packs.Pack, error) {
	if hasAnyFile(pipe.directory, "docker-compose.yaml", "docker-compose.yml") {
		return compose.New(pipe.directory, pipe.network), nil
	}
	return nil, errUnknownPackage
}

func toDomains(services []packs.Service) []string {
	var ans = make([]string, 0, len(services))
	for _, srv := range services {
		ans = append(ans, srv.Domain)
	}
	return ans
}
