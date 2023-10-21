package hashing

import (
	"strings"
	"testing"
)

func TestComputeSHA256(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantHash string
		wantSize int64
	}{
		{
			name:     "empty",
			input:    "",
			wantHash: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			wantSize: 0,
		},
		{
			name:     "hello",
			input:    "hello",
			wantHash: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
			wantSize: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, size, err := ComputeSHA256(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if hash != tt.wantHash {
				t.Errorf("hash = %s, want %s", hash, tt.wantHash)
			}
			if size != tt.wantSize {
				t.Errorf("size = %d, want %d", size, tt.wantSize)
			}
		})
	}
}

func TestBlobDir(t *testing.T) {
	tests := []struct {
		hash string
		want string
	}{
		{"abcdef1234", "ab"},
		{"a", "a"},
		{"", ""},
	}

	for _, tt := range tests {
		got := BlobDir(tt.hash)
		if got != tt.want {
			t.Errorf("BlobDir(%q) = %q, want %q", tt.hash, got, tt.want)
		}
	}
}
