package models

import "time"

type Package struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type Artifact struct {
	ID         int64     `json:"id"`
	PackageID  int64     `json:"package_id"`
	Package    string    `json:"package"`
	Version    string    `json:"version"`
	Hash       string    `json:"hash"`
	Size       int64     `json:"size"`
	UploadedAt time.Time `json:"uploaded_at"`
}

type PackageInfo struct {
	Name     string     `json:"name"`
	Versions []Artifact `json:"versions"`
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}

type UploadResponse struct {
	Package    string `json:"package"`
	Version    string `json:"version"`
	Hash       string `json:"hash"`
	Size       int64  `json:"size"`
	UploadedAt string `json:"uploaded_at"`
}

type GCResult struct {
	DeletedBlobs int   `json:"deleted_blobs"`
	FreedBytes   int64 `json:"freed_bytes"`
}
