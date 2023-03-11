package metadata

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/foundry/registry/internal/core/models"
	"github.com/foundry/registry/internal/core/services"

	_ "modernc.org/sqlite"
)

// SQLiteStore implements MetadataStore backed by SQLite.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens or creates the SQLite database and runs migrations.
func NewSQLiteStore(dataDir string) (*SQLiteStore, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating data directory: %w", err)
	}

	dsn := dataDir + "/registry.db?_journal_mode=WAL&_busy_timeout=5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS packages (
			id   INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE NOT NULL
		);
		CREATE TABLE IF NOT EXISTS artifacts (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			package_id  INTEGER NOT NULL,
			version     TEXT NOT NULL,
			hash        TEXT NOT NULL,
			size        INTEGER NOT NULL,
			uploaded_at DATETIME NOT NULL,
			UNIQUE(package_id, version),
			FOREIGN KEY (package_id) REFERENCES packages(id)
		);
		CREATE INDEX IF NOT EXISTS idx_artifacts_hash ON artifacts(hash);
	`)
	return err
}

func (s *SQLiteStore) CreatePackage(name string) (int64, error) {
	_, err := s.db.Exec("INSERT OR IGNORE INTO packages (name) VALUES (?)", name)
	if err != nil {
		return 0, fmt.Errorf("creating package: %w", err)
	}

	var id int64
	err = s.db.QueryRow("SELECT id FROM packages WHERE name = ?", name).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("getting package id: %w", err)
	}
	return id, nil
}

func (s *SQLiteStore) GetPackage(name string) (*models.Package, error) {
	var pkg models.Package
	err := s.db.QueryRow("SELECT id, name FROM packages WHERE name = ?", name).Scan(&pkg.ID, &pkg.Name)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting package: %w", err)
	}
	return &pkg, nil
}

func (s *SQLiteStore) ListPackages() ([]models.Package, error) {
	rows, err := s.db.Query("SELECT id, name FROM packages ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("listing packages: %w", err)
	}
	defer rows.Close()

	var pkgs []models.Package
	for rows.Next() {
		var p models.Package
		if err := rows.Scan(&p.ID, &p.Name); err != nil {
			return nil, fmt.Errorf("scanning package: %w", err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, rows.Err()
}

func (s *SQLiteStore) SearchPackages(query string) ([]models.Package, error) {
	rows, err := s.db.Query("SELECT id, name FROM packages WHERE name LIKE ? ORDER BY name", "%"+query+"%")
	if err != nil {
		return nil, fmt.Errorf("searching packages: %w", err)
	}
	defer rows.Close()

	var pkgs []models.Package
	for rows.Next() {
		var p models.Package
		if err := rows.Scan(&p.ID, &p.Name); err != nil {
			return nil, fmt.Errorf("scanning package: %w", err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, rows.Err()
}

func (s *SQLiteStore) CreateArtifact(packageID int64, version, hash string, size int64) (*models.Artifact, error) {
	now := time.Now().UTC()
	result, err := s.db.Exec(
		"INSERT INTO artifacts (package_id, version, hash, size, uploaded_at) VALUES (?, ?, ?, ?, ?)",
		packageID, version, hash, size, now,
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return nil, fmt.Errorf("%w: artifact version already exists", services.ErrConflict)
		}
		return nil, fmt.Errorf("creating artifact: %w", err)
	}

	id, _ := result.LastInsertId()
	return &models.Artifact{
		ID:         id,
		PackageID:  packageID,
		Version:    version,
		Hash:       hash,
		Size:       size,
		UploadedAt: now,
	}, nil
}

func (s *SQLiteStore) GetArtifact(packageName, version string) (*models.Artifact, error) {
	var a models.Artifact
	err := s.db.QueryRow(`
		SELECT a.id, a.package_id, p.name, a.version, a.hash, a.size, a.uploaded_at
		FROM artifacts a JOIN packages p ON a.package_id = p.id
		WHERE p.name = ? AND a.version = ?
	`, packageName, version).Scan(&a.ID, &a.PackageID, &a.Package, &a.Version, &a.Hash, &a.Size, &a.UploadedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting artifact: %w", err)
	}
	return &a, nil
}

func (s *SQLiteStore) ListArtifacts(packageName string) ([]models.Artifact, error) {
	rows, err := s.db.Query(`
		SELECT a.id, a.package_id, p.name, a.version, a.hash, a.size, a.uploaded_at
		FROM artifacts a JOIN packages p ON a.package_id = p.id
		WHERE p.name = ?
		ORDER BY a.uploaded_at DESC
	`, packageName)
	if err != nil {
		return nil, fmt.Errorf("listing artifacts: %w", err)
	}
	defer rows.Close()

	var artifacts []models.Artifact
	for rows.Next() {
		var a models.Artifact
		if err := rows.Scan(&a.ID, &a.PackageID, &a.Package, &a.Version, &a.Hash, &a.Size, &a.UploadedAt); err != nil {
			return nil, fmt.Errorf("scanning artifact: %w", err)
		}
		artifacts = append(artifacts, a)
	}
	return artifacts, rows.Err()
}

func (s *SQLiteStore) DeleteArtifact(packageName, version string) error {
	result, err := s.db.Exec(`
		DELETE FROM artifacts WHERE package_id = (
			SELECT id FROM packages WHERE name = ?
		) AND version = ?
	`, packageName, version)
	if err != nil {
		return fmt.Errorf("deleting artifact: %w", err)
	}

	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%w: artifact %s@%s", services.ErrNotFound, packageName, version)
	}
	return nil
}

func (s *SQLiteStore) ReferencedHashes() (map[string]bool, error) {
	rows, err := s.db.Query("SELECT DISTINCT hash FROM artifacts")
	if err != nil {
		return nil, fmt.Errorf("querying referenced hashes: %w", err)
	}
	defer rows.Close()

	refs := make(map[string]bool)
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, fmt.Errorf("scanning hash: %w", err)
		}
		refs[h] = true
	}
	return refs, rows.Err()
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func isUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint failed")
}
