package symmetric

import (
	"context"
	"fmt"

	"github.com/reddec/git-pipe/internal"
)

// Symmetric encryption based on shared key.
// Implementation uses openssl binary with pbkdf2 and aes256 algorithm.
type Symmetric struct {
	Key string
}

func (sc *Symmetric) Encrypt(ctx context.Context, decryptedFile, encryptedFile string) error {
	if err := internal.In(".").Do(ctx, "openssl", "enc", "-e", "-pbkdf2", "-aes256", "-in", decryptedFile, "-out", encryptedFile, "-pass", "stdin").Text(sc.Key).Exec(); err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}
	return nil
}

func (sc *Symmetric) Decrypt(ctx context.Context, encryptedFile, decryptedFile string) error {
	if err := internal.In(".").Do(ctx, "openssl", "enc", "-d", "-pbkdf2", "-aes256", "-in", encryptedFile, "-out", decryptedFile, "-pass", "stdin").Text(sc.Key).Exec(); err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}
	return nil
}
