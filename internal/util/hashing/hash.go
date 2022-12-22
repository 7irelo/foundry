package hashing

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

// ComputeSHA256 reads from r and returns the hex-encoded SHA256 hash and bytes read.
func ComputeSHA256(r io.Reader) (string, int64, error) {
	h := sha256.New()
	n, err := io.Copy(h, r)
	if err != nil {
		return "", 0, fmt.Errorf("computing hash: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// BlobDir returns the two-character prefix directory for a hash.
func BlobDir(hash string) string {
	if len(hash) < 2 {
		return hash
	}
	return hash[:2]
}
