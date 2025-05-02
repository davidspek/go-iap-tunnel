package tunnel

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

// mockTokenSource implements oauth2.TokenSource for testing.
type mockTokenSource struct {
	token *oauth2.Token
	err   error
}

func (m *mockTokenSource) Token() (*oauth2.Token, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.token, nil
}

func TestOAuthTokenProvider_GetHeaders_Success(t *testing.T) {
	token := &oauth2.Token{
		AccessToken: "abc123",
		Expiry:      time.Now().Add(1 * time.Hour),
	}
	provider := NewOAuthTokenProvider(&mockTokenSource{token: token})

	headers, err := provider.GetHeaders()
	require.NoError(t, err)
	assert.Equal(t, "Bearer abc123", headers.Get("Authorization"))
	assert.Equal(t, TUNNEL_CLOUDPROXY_ORIGIN, headers.Get("Origin"))
	assert.Equal(t, TUNNEL_USER_AGENT, headers.Get("User-Agent"))
}

func TestOAuthTokenProvider_GetHeaders_RefreshOnExpired(t *testing.T) {
	expiredToken := &oauth2.Token{
		AccessToken: "expired",
		Expiry:      time.Now().Add(-1 * time.Hour),
	}
	newToken := &oauth2.Token{
		AccessToken: "fresh",
		Expiry:      time.Now().Add(1 * time.Hour),
	}
	provider := NewOAuthTokenProvider(&mockTokenSource{
		token: newToken,
	})
	// Set expired token as currentToken
	provider.SetToken(expiredToken)

	headers, err := provider.GetHeaders()
	require.NoError(t, err)
	assert.Equal(t, "Bearer fresh", headers.Get("Authorization"))
}

func TestOAuthTokenProvider_GetHeaders_Error(t *testing.T) {
	provider := NewOAuthTokenProvider(&mockTokenSource{err: errors.New("fail")})

	_, err := provider.GetHeaders()
	assert.Error(t, err)
}

func TestOAuthTokenProvider_RefreshToken_Success(t *testing.T) {
	token := &oauth2.Token{
		AccessToken: "tok",
		Expiry:      time.Now().Add(1 * time.Hour),
	}
	provider := NewOAuthTokenProvider(&mockTokenSource{token: token})

	err := provider.RefreshToken(context.Background())
	require.NoError(t, err)
	// Check that currentToken is set
	got := provider.GetToken()
	require.NotNil(t, got)
	assert.Equal(t, "tok", got.AccessToken)
}

func TestOAuthTokenProvider_RefreshToken_Error(t *testing.T) {
	provider := NewOAuthTokenProvider(&mockTokenSource{err: errors.New("fail")})

	err := provider.RefreshToken(context.Background())
	assert.Error(t, err)
}
