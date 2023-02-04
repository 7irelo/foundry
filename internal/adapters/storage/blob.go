package storage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/foundry/registry/internal/core/services"
	"github.com/foundry/registry/internal/util/hashing"
)

// DiskBlobStorage stores blobs on disk in a content-addressed layout.
type DiskBlobStorage struct {
	dataDir string
}

// NewDiskBlobStorage creates a new DiskBlobStorage.
func NewDiskBlobStorage(dataDir string) (*DiskBlobStorage, error) {
	blobDir := filepath.Join(dataDir, "blobs")
	if err := os.MkdirAll(blobDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating blob directory: %w", err)
	}
	return &DiskBlobStorage{dataDir: dataDir}, nil
}

// Store streams data from r to disk, computing its SHA256 hash.
// It writes to a temp file first then does an atomic rename.
func (s *DiskBlobStorage) Store(r io.Reader) (string, int64, error) {
	tmpDir := filepath.Join(s.dataDir, "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return "", 0, fmt.Errorf("creating temp directory: %w", err)
	}

	tmp, err := os.CreateTemp(tmpDir, "upload-*")
	if err != nil {
		return "", 0, fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()

	// Ensure cleanup on failure.
	success := false
	defer func() {
		if !success {
			tmp.Close()
			os.Remove(tmpPath)
		}
	}()

	// Stream through SHA256 hasher while writing to temp.
	h, size, err := streamToFile(tmp, r)
	if err != nil {
		return "", 0, err
	}

	if err := tmp.Close(); err != nil {
		return "", 0, fmt.Errorf("closing temp file: %w", err)
	}

	// Move to final content-addressed path.
	dir := filepath.Join(s.dataDir, "blobs", hashing.BlobDir(h))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, fmt.Errorf("creating blob subdirectory: %w", err)
	}

	finalPath := filepath.Join(dir, h)
	if _, err := os.Stat(finalPath); err == nil {
		// Blob already exists, remove the temp.
		os.Remove(tmpPath)
		success = true
		return h, size, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", 0, fmt.Errorf("checking final blob path: %w", err)
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		// A concurrent upload may have already won the race to place the blob.
		if _, statErr := os.Stat(finalPath); statErr == nil {
			os.Remove(tmpPath)
			success = true
			return h, size, nil
		}
		return "", 0, fmt.Errorf("moving blob to final path: %w", err)
	}

	success = true
	return h, size, nil
}

// Open returns a ReadCloser for the blob with the given hash.
func (s *DiskBlobStorage) Open(hash string) (io.ReadCloser, error) {
	p := s.BlobPath(hash)
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: blob %s", services.ErrNotFound, hash)
		}
		return nil, fmt.Errorf("opening blob: %w", err)
	}
	return f, nil
}

// Exists checks if a blob exists.
func (s *DiskBlobStorage) Exists(hash string) bool {
	_, err := os.Stat(s.BlobPath(hash))
	return err == nil
}

// Delete removes a blob.
func (s *DiskBlobStorage) Delete(hash string) error {
	p := s.BlobPath(hash)
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("deleting blob: %w", err)
	}
	return nil
}

// BlobPath returns the full path for a given hash.
func (s *DiskBlobStorage) BlobPath(hash string) string {
	return filepath.Join(s.dataDir, "blobs", hashing.BlobDir(hash), hash)
}

// ListBlobs returns all blob hashes stored on disk.
func (s *DiskBlobStorage) ListBlobs() ([]string, error) {
	blobDir := filepath.Join(s.dataDir, "blobs")
	var hashes []string

	prefixes, err := os.ReadDir(blobDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading blob directory: %w", err)
	}

	for _, prefix := range prefixes {
		if !prefix.IsDir() || len(prefix.Name()) != 2 {
			continue
		}
		subDir := filepath.Join(blobDir, prefix.Name())
		entries, err := os.ReadDir(subDir)
		if err != nil {
			return nil, fmt.Errorf("reading blob subdirectory: %w", err)
		}
		for _, entry := range entries {
			hash := entry.Name()
			if !entry.IsDir() && strings.HasPrefix(hash, prefix.Name()) && isHexHash(hash) {
				hashes = append(hashes, hash)
			}
		}
	}

	return hashes, nil
}

// streamToFile writes from r to f while computing SHA256.
func streamToFile(f *os.File, r io.Reader) (string, int64, error) {
	hasher := newHashingWriter(f)
	n, err := io.Copy(hasher, r)
	if err != nil {
		return "", 0, fmt.Errorf("streaming to file: %w", err)
	}
	return hasher.Hash(), n, nil
}

func isHexHash(v string) bool {
	if len(v) != 64 {
		return false
	}
	for i := 0; i < len(v); i++ {
		ch := v[i]
		if (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') {
			continue
		}
		return false
	}
	return true
}
