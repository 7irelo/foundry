package storage

import (
	"io"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestDiskBlobStorage_StoreAndOpen(t *testing.T) {
	dir := t.TempDir()

	store, err := NewDiskBlobStorage(dir)
	if err != nil {
		t.Fatalf("NewDiskBlobStorage: %v", err)
	}

	content := "hello world"
	hash, size, err := store.Store(strings.NewReader(content))
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	if size != int64(len(content)) {
		t.Errorf("size = %d, want %d", size, len(content))
	}

	if hash == "" {
		t.Fatal("expected non-empty hash")
	}

	// Verify it exists.
	if !store.Exists(hash) {
		t.Error("Exists returned false for stored blob")
	}

	// Open and read.
	rc, err := store.Open(hash)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("reading blob: %v", err)
	}

	if string(data) != content {
		t.Errorf("content = %q, want %q", string(data), content)
	}
}

func TestDiskBlobStorage_Deduplication(t *testing.T) {
	dir := t.TempDir()
	store, err := NewDiskBlobStorage(dir)
	if err != nil {
		t.Fatalf("NewDiskBlobStorage: %v", err)
	}

	content := "deduplicate me"
	hash1, _, _ := store.Store(strings.NewReader(content))
	hash2, _, _ := store.Store(strings.NewReader(content))

	if hash1 != hash2 {
		t.Errorf("hashes differ: %s vs %s", hash1, hash2)
	}

	// Should only have one blob on disk.
	blobs, err := store.ListBlobs()
	if err != nil {
		t.Fatalf("ListBlobs: %v", err)
	}

	count := 0
	for _, b := range blobs {
		if b == hash1 {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 blob, found %d", count)
	}
}

func TestDiskBlobStorage_Delete(t *testing.T) {
	dir := t.TempDir()
	store, err := NewDiskBlobStorage(dir)
	if err != nil {
		t.Fatalf("NewDiskBlobStorage: %v", err)
	}

	hash, _, _ := store.Store(strings.NewReader("to be deleted"))
	if !store.Exists(hash) {
		t.Fatal("blob should exist after store")
	}

	if err := store.Delete(hash); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if store.Exists(hash) {
		t.Error("blob should not exist after delete")
	}
}

func TestDiskBlobStorage_OpenNonExistent(t *testing.T) {
	dir := t.TempDir()
	store, err := NewDiskBlobStorage(dir)
	if err != nil {
		t.Fatalf("NewDiskBlobStorage: %v", err)
	}

	_, err = store.Open("0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Error("expected error opening non-existent blob")
	}
}

func TestDiskBlobStorage_ListBlobs(t *testing.T) {
	dir := t.TempDir()
	store, err := NewDiskBlobStorage(dir)
	if err != nil {
		t.Fatalf("NewDiskBlobStorage: %v", err)
	}

	hash1, _, _ := store.Store(strings.NewReader("file1"))
	hash2, _, _ := store.Store(strings.NewReader("file2"))

	blobs, err := store.ListBlobs()
	if err != nil {
		t.Fatalf("ListBlobs: %v", err)
	}

	found := make(map[string]bool)
	for _, b := range blobs {
		found[b] = true
	}

	if !found[hash1] {
		t.Errorf("missing blob %s", hash1)
	}
	if !found[hash2] {
		t.Errorf("missing blob %s", hash2)
	}
}

func TestDiskBlobStorage_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	store, err := NewDiskBlobStorage(dir)
	if err != nil {
		t.Fatalf("NewDiskBlobStorage: %v", err)
	}

	hash, _, err := store.Store(strings.NewReader("atomic test"))
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Verify no temp files remain.
	tmpDir := dir + "/tmp"
	entries, err := os.ReadDir(tmpDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("reading tmp dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no temp files, found %d", len(entries))
	}

	_ = hash
}

func TestDiskBlobStorage_ConcurrentDedup(t *testing.T) {
	dir := t.TempDir()
	store, err := NewDiskBlobStorage(dir)
	if err != nil {
		t.Fatalf("NewDiskBlobStorage: %v", err)
	}

	const workers = 8
	hashes := make(chan string, workers)
	errs := make(chan error, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hash, _, err := store.Store(strings.NewReader("same-content"))
			if err != nil {
				errs <- err
				return
			}
			hashes <- hash
		}()
	}
	wg.Wait()
	close(errs)
	close(hashes)

	for err := range errs {
		if err != nil {
			t.Fatalf("Store in goroutine failed: %v", err)
		}
	}

	var first string
	for hash := range hashes {
		if first == "" {
			first = hash
			continue
		}
		if hash != first {
			t.Fatalf("hash mismatch in concurrent store: %s vs %s", first, hash)
		}
	}

	blobs, err := store.ListBlobs()
	if err != nil {
		t.Fatalf("ListBlobs: %v", err)
	}

	count := 0
	for _, hash := range blobs {
		if hash == first {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected one blob for concurrent uploads, found %d", count)
	}
}
