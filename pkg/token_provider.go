package tunnel

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

// TokenProvider abstracts authentication operations.
type TokenProvider interface {
	GetHeaders() (http.Header, error)
	RefreshToken(context.Context) error
	SetToken(*oauth2.Token)
	GetToken() *oauth2.Token
}

type OAuthTokenProvider struct {
	ts           oauth2.TokenSource
	currentToken *oauth2.Token
	mu           sync.RWMutex
}

func NewOAuthTokenProvider(ts oauth2.TokenSource) TokenProvider {
	return &OAuthTokenProvider{
		ts: ts,
	}
}

// SetToken sets the current token to the provided token.
// This is useful for testing purposes.
func (p *OAuthTokenProvider) SetToken(token *oauth2.Token) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.currentToken = token
}

// GetToken retrieves the current token.
func (p *OAuthTokenProvider) GetToken() *oauth2.Token {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.currentToken
}

// GetHeaders retrieves the headers required for authentication.
func (p *OAuthTokenProvider) GetHeaders() (http.Header, error) {
	p.mu.RLock()
	token := p.currentToken
	p.mu.RUnlock()

	if token == nil || token.Expiry.Before(time.Now()) {
		var err error
		token, err = p.ts.Token()
		if err != nil {
			return nil, fmt.Errorf("failed to get token: %w", err)
		}
		p.mu.Lock()
		p.currentToken = token
		p.mu.Unlock()
	}

	return http.Header{
		"Origin":        []string{TUNNEL_CLOUDPROXY_ORIGIN},
		"User-Agent":    []string{TUNNEL_USER_AGENT},
		"Authorization": []string{fmt.Sprintf("Bearer %s", token.AccessToken)},
	}, nil
}

// RefreshToken refreshes the current token using the token source.
// It updates the current token in the provider.
func (p *OAuthTokenProvider) RefreshToken(ctx context.Context) error {
	token, err := p.ts.Token()
	if err != nil {
		return fmt.Errorf("failed to refresh token: %w", err)
	}

	p.mu.Lock()
	p.currentToken = token
	p.mu.Unlock()

	return nil
}
