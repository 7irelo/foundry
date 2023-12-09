package metadata

import (
	"errors"
	"os"
	"testing"

	"github.com/foundry/registry/internal/core/services"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	store, err := NewSQLiteStore(dir)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestCreateAndGetPackage(t *testing.T) {
	store := newTestStore(t)

	id, err := store.CreatePackage("mylib")
	if err != nil {
		t.Fatalf("CreatePackage: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	pkg, err := store.GetPackage("mylib")
	if err != nil {
		t.Fatalf("GetPackage: %v", err)
	}
	if pkg == nil {
		t.Fatal("expected package, got nil")
	}
	if pkg.Name != "mylib" {
		t.Errorf("name = %q, want %q", pkg.Name, "mylib")
	}
}

func TestCreatePackageIdempotent(t *testing.T) {
	store := newTestStore(t)

	id1, _ := store.CreatePackage("pkg1")
	id2, _ := store.CreatePackage("pkg1")

	if id1 != id2 {
		t.Errorf("expected same id, got %d and %d", id1, id2)
	}
}

func TestGetPackageNotFound(t *testing.T) {
	store := newTestStore(t)

	pkg, err := store.GetPackage("nonexistent")
	if err != nil {
		t.Fatalf("GetPackage: %v", err)
	}
	if pkg != nil {
		t.Error("expected nil for nonexistent package")
	}
}

func TestListPackages(t *testing.T) {
	store := newTestStore(t)

	store.CreatePackage("alpha")
	store.CreatePackage("beta")
	store.CreatePackage("gamma")

	pkgs, err := store.ListPackages()
	if err != nil {
		t.Fatalf("ListPackages: %v", err)
	}
	if len(pkgs) != 3 {
		t.Errorf("expected 3 packages, got %d", len(pkgs))
	}
}

func TestSearchPackages(t *testing.T) {
	store := newTestStore(t)

	store.CreatePackage("my-app")
	store.CreatePackage("my-lib")
	store.CreatePackage("other")

	pkgs, err := store.SearchPackages("my")
	if err != nil {
		t.Fatalf("SearchPackages: %v", err)
	}
	if len(pkgs) != 2 {
		t.Errorf("expected 2 packages, got %d", len(pkgs))
	}
}

func TestCreateAndGetArtifact(t *testing.T) {
	store := newTestStore(t)

	pkgID, _ := store.CreatePackage("mylib")
	artifact, err := store.CreateArtifact(pkgID, "1.0.0", "abc123", 1024)
	if err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}
	if artifact.Version != "1.0.0" {
		t.Errorf("version = %q, want %q", artifact.Version, "1.0.0")
	}

	got, err := store.GetArtifact("mylib", "1.0.0")
	if err != nil {
		t.Fatalf("GetArtifact: %v", err)
	}
	if got == nil {
		t.Fatal("expected artifact, got nil")
	}
	if got.Hash != "abc123" {
		t.Errorf("hash = %q, want %q", got.Hash, "abc123")
	}
}

func TestGetArtifactNotFound(t *testing.T) {
	store := newTestStore(t)

	artifact, err := store.GetArtifact("missing", "1.0.0")
	if err != nil {
		t.Fatalf("GetArtifact: %v", err)
	}
	if artifact != nil {
		t.Error("expected nil for nonexistent artifact")
	}
}

func TestCreateDuplicateArtifact(t *testing.T) {
	store := newTestStore(t)

	pkgID, _ := store.CreatePackage("mylib")
	store.CreateArtifact(pkgID, "1.0.0", "hash1", 100)
	_, err := store.CreateArtifact(pkgID, "1.0.0", "hash2", 200)
	if err == nil {
		t.Error("expected error for duplicate version")
	}
	if !errors.Is(err, services.ErrConflict) {
		t.Errorf("expected ErrConflict, got %v", err)
	}
}

func TestListArtifacts(t *testing.T) {
	store := newTestStore(t)

	pkgID, _ := store.CreatePackage("mylib")
	store.CreateArtifact(pkgID, "1.0.0", "hash1", 100)
	store.CreateArtifact(pkgID, "2.0.0", "hash2", 200)

	artifacts, err := store.ListArtifacts("mylib")
	if err != nil {
		t.Fatalf("ListArtifacts: %v", err)
	}
	if len(artifacts) != 2 {
		t.Errorf("expected 2 artifacts, got %d", len(artifacts))
	}
}

func TestDeleteArtifact(t *testing.T) {
	store := newTestStore(t)

	pkgID, _ := store.CreatePackage("mylib")
	store.CreateArtifact(pkgID, "1.0.0", "hash1", 100)

	err := store.DeleteArtifact("mylib", "1.0.0")
	if err != nil {
		t.Fatalf("DeleteArtifact: %v", err)
	}

	artifact, _ := store.GetArtifact("mylib", "1.0.0")
	if artifact != nil {
		t.Error("artifact should be deleted")
	}
}

func TestDeleteArtifactNotFound(t *testing.T) {
	store := newTestStore(t)

	err := store.DeleteArtifact("missing", "1.0.0")
	if err == nil {
		t.Error("expected error deleting nonexistent artifact")
	}
	if !errors.Is(err, services.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestReferencedHashes(t *testing.T) {
	store := newTestStore(t)

	pkgID, _ := store.CreatePackage("mylib")
	store.CreateArtifact(pkgID, "1.0.0", "hash1", 100)
	store.CreateArtifact(pkgID, "2.0.0", "hash2", 200)

	// Different package, same hash (dedup).
	pkgID2, _ := store.CreatePackage("otherlib")
	store.CreateArtifact(pkgID2, "1.0.0", "hash1", 100)

	refs, err := store.ReferencedHashes()
	if err != nil {
		t.Fatalf("ReferencedHashes: %v", err)
	}
	if len(refs) != 2 {
		t.Errorf("expected 2 unique hashes, got %d", len(refs))
	}
	if !refs["hash1"] || !refs["hash2"] {
		t.Error("missing expected hash")
	}
}

func TestSQLiteStoreDataDir(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSQLiteStore(dir)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	store.Close()

	// Verify the database file was created.
	if _, err := os.Stat(dir + "/registry.db"); os.IsNotExist(err) {
		t.Error("expected registry.db to exist")
	}
}
