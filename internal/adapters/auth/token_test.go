package auth

import "testing"

func TestTokenAuth_ValidateToken(t *testing.T) {
	auth := NewTokenAuth([]string{"token1", "token2"})

	if !auth.ValidateToken("token1") {
		t.Error("token1 should be valid")
	}
	if !auth.ValidateToken("token2") {
		t.Error("token2 should be valid")
	}
	if auth.ValidateToken("token3") {
		t.Error("token3 should be invalid")
	}
	if auth.ValidateToken("") {
		t.Error("empty token should be invalid")
	}
}

func TestTokenAuth_EmptyTokenList(t *testing.T) {
	auth := NewTokenAuth([]string{})
	if auth.ValidateToken("anything") {
		t.Error("no tokens configured, nothing should validate")
	}
}
