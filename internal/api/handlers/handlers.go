package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/foundry/registry/internal/core/models"
	"github.com/foundry/registry/internal/core/services"
	"github.com/foundry/registry/internal/util/logging"
)

// Handler holds all HTTP handlers and their dependencies.
type Handler struct {
	blobs       services.BlobStorage
	meta        services.MetadataStore
	auth        services.Authenticator
	logger      zerolog.Logger
	locksMu     sync.Mutex
	uploadLocks map[string]*artifactLock
}

// New creates a new Handler with the given dependencies.
func New(blobs services.BlobStorage, meta services.MetadataStore, auth services.Authenticator, logger zerolog.Logger) *Handler {
	return &Handler{
		blobs:       blobs,
		meta:        meta,
		auth:        auth,
		logger:      logger,
		uploadLocks: make(map[string]*artifactLock),
	}
}

// Router returns the chi router with all routes.
func (h *Handler) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(h.requestIDMiddleware)
	r.Use(h.loggingMiddleware)
	r.Use(h.authMiddleware)

	r.Post("/api/v1/artifacts/{package}/{version}", h.UploadArtifact)
	r.Get("/api/v1/artifacts/{package}/{version}", h.DownloadArtifact)
	r.Get("/api/v1/packages", h.ListPackages)
	r.Get("/api/v1/packages/{package}", h.GetPackage)
	r.Delete("/api/v1/artifacts/{package}/{version}", h.DeleteArtifact)
	r.Post("/api/v1/gc", h.GarbageCollect)

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "route not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	})

	return r
}

// requestIDMiddleware adds a unique request ID to each request.
func (h *Handler) requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := uuid.NewString()
		ctx := logging.WithRequestID(r.Context(), id)
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// loggingMiddleware logs each request.
func (h *Handler) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		logging.LogRequest(h.logger, r.Context(), r.Method, r.URL.Path, rw.status, rw.written, time.Since(start))
	})
}

// authMiddleware validates the bearer token.
func (h *Handler) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(header, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "missing or invalid authorization header")
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		if !h.auth.ValidateToken(token) {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// UploadArtifact handles POST /api/v1/artifacts/{package}/{version}
func (h *Handler) UploadArtifact(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	pkgName := chi.URLParam(r, "package")
	version := chi.URLParam(r, "version")

	if pkgName == "" || version == "" {
		writeError(w, http.StatusBadRequest, "package and version are required")
		return
	}

	unlock := h.lockArtifactUpload(pkgName, version)
	defer unlock()

	// Check for existing artifact.
	existing, err := h.meta.GetArtifact(pkgName, version)
	if err != nil {
		h.logger.Error().Err(err).Msg("checking existing artifact")
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if existing != nil {
		writeError(w, http.StatusConflict, fmt.Sprintf("artifact %s@%s already exists", pkgName, version))
		return
	}

	// Stream the upload to blob storage.
	hash, size, err := h.blobs.Store(r.Body)
	if err != nil {
		h.logger.Error().Err(err).Msg("storing blob")
		writeError(w, http.StatusInternalServerError, "failed to store artifact")
		return
	}

	h.logger.Info().
		Str("request_id", logging.RequestID(r.Context())).
		Str("package", pkgName).
		Str("version", version).
		Str("hash", hash).
		Int64("size", size).
		Msg("blob stored")

	// Store metadata.
	pkgID, err := h.meta.CreatePackage(pkgName)
	if err != nil {
		h.logger.Error().Err(err).Msg("creating package")
		writeError(w, http.StatusInternalServerError, "failed to create package")
		return
	}

	artifact, err := h.meta.CreateArtifact(pkgID, version, hash, size)
	if err != nil {
		if errors.Is(err, services.ErrConflict) {
			writeError(w, http.StatusConflict, fmt.Sprintf("artifact %s@%s already exists", pkgName, version))
			return
		}
		h.logger.Error().Err(err).Msg("creating artifact")
		writeError(w, http.StatusInternalServerError, "failed to create artifact metadata")
		return
	}

	h.logger.Info().
		Str("request_id", logging.RequestID(r.Context())).
		Str("package", pkgName).
		Str("version", version).
		Str("hash", artifact.Hash).
		Int64("size", artifact.Size).
		Dur("upload_latency", time.Since(start)).
		Msg("artifact upload completed")

	writeJSON(w, http.StatusCreated, models.UploadResponse{
		Package:    pkgName,
		Version:    version,
		Hash:       artifact.Hash,
		Size:       artifact.Size,
		UploadedAt: artifact.UploadedAt.Format(time.RFC3339),
	})
}

// DownloadArtifact handles GET /api/v1/artifacts/{package}/{version}
func (h *Handler) DownloadArtifact(w http.ResponseWriter, r *http.Request) {
	pkgName := chi.URLParam(r, "package")
	version := chi.URLParam(r, "version")

	artifact, err := h.meta.GetArtifact(pkgName, version)
	if err != nil {
		h.logger.Error().Err(err).Msg("getting artifact")
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if artifact == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("artifact %s@%s not found", pkgName, version))
		return
	}

	reader, err := h.blobs.Open(artifact.Hash)
	if err != nil {
		if errors.Is(err, services.ErrNotFound) {
			writeError(w, http.StatusNotFound, "artifact blob missing on disk")
			return
		}
		h.logger.Error().Err(err).Str("hash", artifact.Hash).Msg("opening blob")
		writeError(w, http.StatusInternalServerError, "blob not found on disk")
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", artifact.Size))
	w.Header().Set("X-Artifact-Hash", artifact.Hash)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s-%s\"", pkgName, version))
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, reader); err != nil {
		h.logger.Error().
			Err(err).
			Str("request_id", logging.RequestID(r.Context())).
			Str("package", pkgName).
			Str("version", version).
			Msg("streaming artifact response")
	}
}

// ListPackages handles GET /api/v1/packages
func (h *Handler) ListPackages(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("search")

	var pkgs []models.Package
	var err error
	if query != "" {
		pkgs, err = h.meta.SearchPackages(query)
	} else {
		pkgs, err = h.meta.ListPackages()
	}

	if err != nil {
		h.logger.Error().Err(err).Msg("listing packages")
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if pkgs == nil {
		pkgs = []models.Package{}
	}
	writeJSON(w, http.StatusOK, pkgs)
}

// GetPackage handles GET /api/v1/packages/{package}
func (h *Handler) GetPackage(w http.ResponseWriter, r *http.Request) {
	pkgName := chi.URLParam(r, "package")

	pkg, err := h.meta.GetPackage(pkgName)
	if err != nil {
		h.logger.Error().Err(err).Msg("getting package")
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if pkg == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("package %s not found", pkgName))
		return
	}

	artifacts, err := h.meta.ListArtifacts(pkgName)
	if err != nil {
		h.logger.Error().Err(err).Msg("listing artifacts")
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if artifacts == nil {
		artifacts = []models.Artifact{}
	}
	writeJSON(w, http.StatusOK, models.PackageInfo{
		Name:     pkg.Name,
		Versions: artifacts,
	})
}

// DeleteArtifact handles DELETE /api/v1/artifacts/{package}/{version}
func (h *Handler) DeleteArtifact(w http.ResponseWriter, r *http.Request) {
	pkgName := chi.URLParam(r, "package")
	version := chi.URLParam(r, "version")

	if err := h.meta.DeleteArtifact(pkgName, version); err != nil {
		if errors.Is(err, services.ErrNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		h.logger.Error().Err(err).Msg("deleting artifact")
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// GarbageCollect handles POST /api/v1/gc
func (h *Handler) GarbageCollect(w http.ResponseWriter, r *http.Request) {
	referenced, err := h.meta.ReferencedHashes()
	if err != nil {
		h.logger.Error().Err(err).Msg("getting referenced hashes")
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	blobs, err := h.blobs.ListBlobs()
	if err != nil {
		h.logger.Error().Err(err).Msg("listing blobs")
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var deleted int
	var freed int64
	for _, hash := range blobs {
		if referenced[hash] {
			continue
		}

		path := h.blobs.BlobPath(hash)
		info, err := os.Stat(path)
		if err == nil {
			freed += info.Size()
		}

		if err := h.blobs.Delete(hash); err != nil {
			h.logger.Error().Err(err).Str("hash", hash).Msg("deleting unreferenced blob")
			continue
		}
		deleted++
		h.logger.Info().Str("hash", hash).Msg("garbage collected blob")
	}

	writeJSON(w, http.StatusOK, models.GCResult{
		DeletedBlobs: deleted,
		FreedBytes:   freed,
	})
}

// Helper functions

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, models.ErrorResponse{
		Error:   http.StatusText(status),
		Code:    status,
		Message: msg,
	})
}

// responseWriter wraps http.ResponseWriter to capture status and bytes written.
type responseWriter struct {
	http.ResponseWriter
	status  int
	written int64
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.written += int64(n)
	return n, err
}

func (h *Handler) lockArtifactUpload(pkgName, version string) func() {
	key := pkgName + "@" + version
	h.locksMu.Lock()
	lock, ok := h.uploadLocks[key]
	if !ok {
		lock = &artifactLock{}
		h.uploadLocks[key] = lock
	}
	lock.refs++
	h.locksMu.Unlock()

	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		h.locksMu.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(h.uploadLocks, key)
		}
		h.locksMu.Unlock()
	}
}

type artifactLock struct {
	mu   sync.Mutex
	refs int
}
