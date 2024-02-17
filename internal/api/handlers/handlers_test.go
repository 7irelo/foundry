package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/rs/zerolog"

	"github.com/foundry/registry/internal/adapters/auth"
	"github.com/foundry/registry/internal/adapters/metadata"
	"github.com/foundry/registry/internal/adapters/storage"
)

func setupTestHandler(t *testing.T) (*Handler, http.Handler) {
	t.Helper()
	dir := t.TempDir()

	blobs, err := storage.NewDiskBlobStorage(dir)
	if err != nil {
		t.Fatalf("NewDiskBlobStorage: %v", err)
	}

	meta, err := metadata.NewSQLiteStore(dir)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { meta.Close() })

	authenticator := auth.NewTokenAuth([]string{"test-token"})
	logger := zerolog.Nop()

	h := New(blobs, meta, authenticator, logger)
	return h, h.Router()
}

func doRequest(t *testing.T, router http.Handler, method, path, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

func TestAuthRequired(t *testing.T) {
	_, router := setupTestHandler(t)

	rr := doRequest(t, router, "GET", "/api/v1/packages", "", nil)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestInvalidToken(t *testing.T) {
	_, router := setupTestHandler(t)

	rr := doRequest(t, router, "GET", "/api/v1/packages", "bad-token", nil)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestListPackagesEmpty(t *testing.T) {
	_, router := setupTestHandler(t)

	rr := doRequest(t, router, "GET", "/api/v1/packages", "test-token", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var pkgs []map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&pkgs)
	if len(pkgs) != 0 {
		t.Errorf("expected empty list, got %d", len(pkgs))
	}
}

func TestUploadAndDownload(t *testing.T) {
	_, router := setupTestHandler(t)

	content := []byte("test artifact content")

	// Upload.
	rr := doRequest(t, router, "POST", "/api/v1/artifacts/mylib/1.0.0", "test-token", content)
	if rr.Code != http.StatusCreated {
		t.Fatalf("upload: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var uploadResp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&uploadResp)
	if uploadResp["package"] != "mylib" {
		t.Errorf("package = %v, want mylib", uploadResp["package"])
	}
	if uploadResp["hash"] == "" {
		t.Error("expected non-empty hash")
	}

	// Download.
	rr = doRequest(t, router, "GET", "/api/v1/artifacts/mylib/1.0.0", "test-token", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("download: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	if rr.Body.String() != string(content) {
		t.Errorf("content = %q, want %q", rr.Body.String(), string(content))
	}
}

func TestUploadDuplicate(t *testing.T) {
	_, router := setupTestHandler(t)

	content := []byte("data")
	doRequest(t, router, "POST", "/api/v1/artifacts/mylib/1.0.0", "test-token", content)

	rr := doRequest(t, router, "POST", "/api/v1/artifacts/mylib/1.0.0", "test-token", content)
	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rr.Code)
	}
}

func TestDownloadNotFound(t *testing.T) {
	_, router := setupTestHandler(t)

	rr := doRequest(t, router, "GET", "/api/v1/artifacts/mylib/1.0.0", "test-token", nil)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestGetPackageInfo(t *testing.T) {
	_, router := setupTestHandler(t)

	doRequest(t, router, "POST", "/api/v1/artifacts/mylib/1.0.0", "test-token", []byte("v1"))
	doRequest(t, router, "POST", "/api/v1/artifacts/mylib/2.0.0", "test-token", []byte("v2"))

	rr := doRequest(t, router, "GET", "/api/v1/packages/mylib", "test-token", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var info map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&info)
	versions := info["versions"].([]interface{})
	if len(versions) != 2 {
		t.Errorf("expected 2 versions, got %d", len(versions))
	}
}

func TestDeleteArtifactHandler(t *testing.T) {
	_, router := setupTestHandler(t)

	doRequest(t, router, "POST", "/api/v1/artifacts/mylib/1.0.0", "test-token", []byte("data"))

	rr := doRequest(t, router, "DELETE", "/api/v1/artifacts/mylib/1.0.0", "test-token", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify it's gone.
	rr = doRequest(t, router, "GET", "/api/v1/artifacts/mylib/1.0.0", "test-token", nil)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", rr.Code)
	}
}

func TestDeleteNotFound(t *testing.T) {
	_, router := setupTestHandler(t)

	rr := doRequest(t, router, "DELETE", "/api/v1/artifacts/mylib/1.0.0", "test-token", nil)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestGarbageCollect(t *testing.T) {
	_, router := setupTestHandler(t)

	// Upload and then delete to create an orphaned blob.
	doRequest(t, router, "POST", "/api/v1/artifacts/mylib/1.0.0", "test-token", []byte("gc-test"))
	doRequest(t, router, "DELETE", "/api/v1/artifacts/mylib/1.0.0", "test-token", nil)

	rr := doRequest(t, router, "POST", "/api/v1/gc", "test-token", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var result map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&result)
	if result["deleted_blobs"].(float64) < 1 {
		t.Error("expected at least 1 deleted blob")
	}
}

func TestSearchPackages(t *testing.T) {
	_, router := setupTestHandler(t)

	doRequest(t, router, "POST", "/api/v1/artifacts/my-app/1.0.0", "test-token", []byte("a"))
	doRequest(t, router, "POST", "/api/v1/artifacts/my-lib/1.0.0", "test-token", []byte("b"))
	doRequest(t, router, "POST", "/api/v1/artifacts/other/1.0.0", "test-token", []byte("c"))

	rr := doRequest(t, router, "GET", "/api/v1/packages?search=my", "test-token", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var pkgs []map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&pkgs)
	if len(pkgs) != 2 {
		t.Errorf("expected 2 packages, got %d", len(pkgs))
	}
}

func TestRouteNotFoundJSON(t *testing.T) {
	_, router := setupTestHandler(t)

	rr := doRequest(t, router, "GET", "/api/v1/does-not-exist", "test-token", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type = %q, want application/json", got)
	}
	var payload map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode json: %v", err)
	}
	if payload["message"] != "route not found" {
		t.Fatalf("message = %v, want route not found", payload["message"])
	}
}

func TestDownloadMissingBlobReturnsNotFound(t *testing.T) {
	h, router := setupTestHandler(t)

	doRequest(t, router, "POST", "/api/v1/artifacts/mylib/1.0.0", "test-token", []byte("data"))
	artifact, err := h.meta.GetArtifact("mylib", "1.0.0")
	if err != nil {
		t.Fatalf("GetArtifact: %v", err)
	}
	if artifact == nil {
		t.Fatal("expected artifact metadata")
	}
	if err := h.blobs.Delete(artifact.Hash); err != nil {
		t.Fatalf("Delete blob: %v", err)
	}

	rr := doRequest(t, router, "GET", "/api/v1/artifacts/mylib/1.0.0", "test-token", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when blob missing, got %d", rr.Code)
	}
}

func TestConcurrentUploadSameVersion(t *testing.T) {
	_, router := setupTestHandler(t)

	const workers = 2
	start := make(chan struct{})
	var wg sync.WaitGroup
	codes := make(chan int, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			rr := doRequest(t, router, "POST", "/api/v1/artifacts/concurrent/1.0.0", "test-token", []byte("same"))
			codes <- rr.Code
		}()
	}

	close(start)
	wg.Wait()
	close(codes)

	var created int
	var conflict int
	for code := range codes {
		if code == http.StatusCreated {
			created++
		}
		if code == http.StatusConflict {
			conflict++
		}
		if code >= 500 {
			t.Fatalf("unexpected server error code: %d", code)
		}
	}

	if created != 1 || conflict != 1 {
		t.Fatalf("expected one created and one conflict, got created=%d conflict=%d", created, conflict)
	}
}
