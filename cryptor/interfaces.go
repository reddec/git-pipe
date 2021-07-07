package cryptor

import "context"

// Cryptor provides sub-system to encrypt and decrypt files (backup mostly).
// Files are managed outside of cryptor. Implementations may not care about removal or creation of the provided files.
// Implementation must expect, that files could be large (bigger then RAM).
type Cryptor interface {
	// Encrypt source file and write encrypted content to destination file.
	Encrypt(ctx context.Context, sourceFile, destinationFile string) error
	// Decrypt source file and write decrypted content to destination file.
	Decrypt(ctx context.Context, sourceFile, destinationFile string) error
}
