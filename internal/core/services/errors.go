package services

import "errors"

var (
	// ErrNotFound indicates a requested resource does not exist.
	ErrNotFound = errors.New("not found")
	// ErrConflict indicates a uniqueness or state conflict.
	ErrConflict = errors.New("conflict")
)
