package nobackup

import (
	"context"

	"github.com/reddec/git-pipe/backup"
)

type NoBackup struct{}

func (nb *NoBackup) Backup(ctx context.Context, name string, sourceFile string) error {
	return nil
}

func (nb *NoBackup) Restore(ctx context.Context, name string, targetFile string) error {
	return backup.ErrBackupNotExists
}
