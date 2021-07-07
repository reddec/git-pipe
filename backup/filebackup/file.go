package filebackup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/reddec/git-pipe/backup"
)

const defaultPermission = 0700

type FileBackup struct {
	Directory string
}

func (fb *FileBackup) Backup(ctx context.Context, name string, sourceFile string) error {
	in, err := os.Open(sourceFile)

	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer in.Close()

	err = os.MkdirAll(fb.Directory, defaultPermission)
	if err != nil {
		return fmt.Errorf("create backup dir %s: %w", fb.Directory, err)
	}

	tmp := filepath.Join(fb.Directory, name+".!tmp")
	dest := filepath.Join(fb.Directory, name)

	out, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return fmt.Errorf("copy content: %w", err)
	}

	err = out.Close()
	if err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmp, dest); err != nil {
		return fmt.Errorf("rename temp to destination: %w", err)
	}

	return nil
}

func (fb *FileBackup) Restore(ctx context.Context, name string, targetFile string) error {
	sourceFile := filepath.Join(fb.Directory, name)
	in, err := os.Open(sourceFile)
	if errors.Is(err, os.ErrNotExist) {
		return backup.ErrBackupNotExists
	}
	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer in.Close()

	out, err := os.Create(targetFile)
	if err != nil {
		return fmt.Errorf("create dest file: %w", err)
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return fmt.Errorf("copy content: %w", err)
	}

	if err := out.Close(); err != nil {
		return fmt.Errorf("close file: %w", err)
	}

	return nil
}
