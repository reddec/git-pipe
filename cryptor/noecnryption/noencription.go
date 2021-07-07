package noecnryption

import (
	"context"
	"fmt"
	"os"
)

// NoEncryption is mock implementation of cryptor that does nothing with content.
type NoEncryption struct {
}

func (ne *NoEncryption) Encrypt(ctx context.Context, decryptedFile, encryptedFile string) error {
	if err := os.Rename(decryptedFile, encryptedFile); err != nil {
		return fmt.Errorf("rename file: %w", err)
	}
	return nil
}

func (ne *NoEncryption) Decrypt(ctx context.Context, encryptedFile, decryptedFile string) error {
	if err := os.Rename(encryptedFile, decryptedFile); err != nil {
		return fmt.Errorf("rename file: %w", err)
	}
	return nil
}
