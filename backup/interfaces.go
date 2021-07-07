package backup

import (
	"context"
	"errors"
)

var ErrBackupNotExists = errors.New("backup not exists")

// Backup provider.
type Backup interface {
	// Backup source file in storage under defined name.
	Backup(ctx context.Context, name string, sourceFile string) error
	// Restore backup with defined name to target file.
	// Must return ErrBackupNotExists in case there is no backup with such name.
	Restore(ctx context.Context, name string, targetFile string) error
}
