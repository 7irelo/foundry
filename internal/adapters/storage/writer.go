package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
)

// hashingWriter wraps a writer and computes SHA256 as data passes through.
type hashingWriter struct {
	w io.Writer
	h hash.Hash
}

func newHashingWriter(w io.Writer) *hashingWriter {
	return &hashingWriter{
		w: w,
		h: sha256.New(),
	}
}

func (hw *hashingWriter) Write(p []byte) (int, error) {
	n, err := hw.w.Write(p)
	if n > 0 {
		hw.h.Write(p[:n])
	}
	return n, err
}

func (hw *hashingWriter) Hash() string {
	return hex.EncodeToString(hw.h.Sum(nil))
}
