package services

import (
	"io"

	"github.com/foundry/registry/internal/core/models"
)

// BlobStorage handles content-addressed blob storage on disk.
type BlobStorage interface {
	// Store streams data to disk, computing its SHA256 hash.
	// Returns the hex-encoded hash and total bytes written.
	Store(r io.Reader) (hash string, size int64, err error)

	// Open returns a ReadCloser for the blob with the given hash.
	Open(hash string) (io.ReadCloser, error)

	// Exists checks if a blob with the given hash exists.
	Exists(hash string) bool

	// Delete removes a blob by hash.
	Delete(hash string) error

	// BlobPath returns the full path for a given hash.
	BlobPath(hash string) string

	// ListBlobs returns all blob hashes on disk.
	ListBlobs() ([]string, error)
}

// MetadataStore handles artifact metadata in a database.
type MetadataStore interface {
	// CreatePackage creates a package if it doesn't exist, returns its ID.
	CreatePackage(name string) (int64, error)

	// GetPackage retrieves a package by name.
	GetPackage(name string) (*models.Package, error)

	// ListPackages returns all packages.
	ListPackages() ([]models.Package, error)

	// SearchPackages searches packages by name substring.
	SearchPackages(query string) ([]models.Package, error)

	// CreateArtifact stores artifact metadata.
	CreateArtifact(packageID int64, version, hash string, size int64) (*models.Artifact, error)

	// GetArtifact retrieves an artifact by package name and version.
	GetArtifact(packageName, version string) (*models.Artifact, error)

	// ListArtifacts lists all artifacts for a package.
	ListArtifacts(packageName string) ([]models.Artifact, error)

	// DeleteArtifact deletes an artifact by package name and version.
	DeleteArtifact(packageName, version string) error

	// ReferencedHashes returns all hashes referenced by artifacts.
	ReferencedHashes() (map[string]bool, error)

	// Close closes the metadata store.
	Close() error
}

// Authenticator validates request tokens.
type Authenticator interface {
	// ValidateToken checks if a token is valid.
	ValidateToken(token string) bool
}
