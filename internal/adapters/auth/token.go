package auth

// TokenAuth validates tokens against a static list.
type TokenAuth struct {
	tokens map[string]bool
}

// NewTokenAuth creates a new TokenAuth from a list of valid tokens.
func NewTokenAuth(tokens []string) *TokenAuth {
	m := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		m[t] = true
	}
	return &TokenAuth{tokens: m}
}

// ValidateToken returns true if the token is in the allowed list.
func (a *TokenAuth) ValidateToken(token string) bool {
	return a.tokens[token]
}
