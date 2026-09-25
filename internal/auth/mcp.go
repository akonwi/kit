package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

const (
	mcpOAuthEntryType      = "mcp_oauth"
	mcpOAuthTombstoneType  = "mcp_oauth_tombstone"
	maximumMCPServerKeyLen = 512
	maximumOAuthValueLen   = 64 << 10
	maximumOAuthScopes     = 128
)

// MCPOAuthSession is the durable OAuth state needed to restore and refresh one
// MCP authorization-code session. It never leaves the private auth store.
type MCPOAuthSession struct {
	Config oauth2.Config
	Token  oauth2.Token
	// Issuer binds dynamically registered client credentials to the
	// authorization server that issued them.
	Issuer string
}

// MCPOAuthRecord is one loaded MCP OAuth credential generation.
type MCPOAuthRecord struct {
	Session  MCPOAuthSession
	Revision string
	Found    bool
}

type mcpOAuthEntry struct {
	Type         string           `json:"type"`
	Revision     string           `json:"revision"`
	ClientID     string           `json:"clientId"`
	ClientSecret string           `json:"clientSecret,omitempty"`
	RedirectURL  string           `json:"redirectUrl"`
	Scopes       []string         `json:"scopes,omitempty"`
	AuthURL      string           `json:"authUrl"`
	TokenURL     string           `json:"tokenUrl"`
	AuthStyle    oauth2.AuthStyle `json:"authStyle,omitempty"`
	Issuer       string           `json:"issuer,omitempty"`
	AccessToken  string           `json:"accessToken"`
	TokenType    string           `json:"tokenType,omitempty"`
	RefreshToken string           `json:"refreshToken,omitempty"`
	Expiry       time.Time        `json:"expiry,omitempty"`
}

// LoadMCPOAuth loads one server's saved OAuth generation.
func (s *Store) LoadMCPOAuth(ctx context.Context, serverKey string) (MCPOAuthRecord, error) {
	if err := validateMCPServerKey(serverKey); err != nil {
		return MCPOAuthRecord{}, err
	}
	var record MCPOAuthRecord
	err := s.withLock(ctx, func() error {
		entries, err := readAuthEntries(s.path)
		if err != nil {
			return err
		}
		raw, ok := entries[serverKey]
		if !ok {
			return nil
		}
		var entry mcpOAuthEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return fmt.Errorf("auth: decode MCP OAuth credential: %w", err)
		}
		if entry.Type == mcpOAuthTombstoneType {
			if err := validateMCPRevision(entry.Revision); err != nil {
				return err
			}
			record.Revision = entry.Revision
			return nil
		}
		session, err := decodeMCPOAuthEntry(entry)
		if err != nil {
			return err
		}
		record = MCPOAuthRecord{Session: session, Revision: entry.Revision, Found: true}
		return nil
	})
	return record, err
}

// SaveMCPOAuth atomically replaces one server's credentials if expectedRevision
// is still current. An empty revision creates a record only when none exists.
func (s *Store) SaveMCPOAuth(ctx context.Context, serverKey, expectedRevision string, session MCPOAuthSession) (string, error) {
	if err := validateMCPServerKey(serverKey); err != nil {
		return "", err
	}
	if err := validateMCPOAuthSession(session); err != nil {
		return "", err
	}
	nextRevision, err := randomRevision()
	if err != nil {
		return "", err
	}
	err = s.withLock(ctx, func() error {
		entries, err := readAuthEntries(s.path)
		if err != nil {
			return err
		}
		currentRevision, err := currentMCPRevision(entries, serverKey)
		if err != nil {
			return err
		}
		if currentRevision != expectedRevision {
			return ErrCredentialsChanged
		}
		entry := encodeMCPOAuthEntry(session, nextRevision)
		raw, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("auth: encode MCP OAuth credential: %w", err)
		}
		entries[serverKey] = raw
		return writeAuthEntries(s.path, entries)
	})
	if err != nil {
		return "", err
	}
	return nextRevision, nil
}

// DeleteMCPOAuth clears one server and advances its generation. The tombstone
// prevents an in-flight refresh or login from resurrecting credentials.
func (s *Store) DeleteMCPOAuth(ctx context.Context, serverKey string) error {
	_, err := s.deleteMCPOAuth(ctx, serverKey, "", false)
	return err
}

// DeleteMCPOAuthRevision clears credentials only if expectedRevision remains
// current. It is used when a request rejects a previously loaded generation.
func (s *Store) DeleteMCPOAuthRevision(ctx context.Context, serverKey, expectedRevision string) (string, error) {
	if expectedRevision == "" {
		return "", ErrCredentialsChanged
	}
	return s.deleteMCPOAuth(ctx, serverKey, expectedRevision, true)
}

func (s *Store) deleteMCPOAuth(ctx context.Context, serverKey, expectedRevision string, guarded bool) (string, error) {
	if err := validateMCPServerKey(serverKey); err != nil {
		return "", err
	}
	nextRevision, err := randomRevision()
	if err != nil {
		return "", err
	}
	err = s.withLock(ctx, func() error {
		entries, err := readAuthEntries(s.path)
		if err != nil {
			return err
		}
		currentRevision, err := currentMCPRevision(entries, serverKey)
		if err != nil {
			return err
		}
		if guarded && currentRevision != expectedRevision {
			return ErrCredentialsChanged
		}
		raw, err := json.Marshal(mcpOAuthEntry{Type: mcpOAuthTombstoneType, Revision: nextRevision})
		if err != nil {
			return err
		}
		entries[serverKey] = raw
		return writeAuthEntries(s.path, entries)
	})
	if err != nil {
		return "", err
	}
	return nextRevision, nil
}

func validateMCPServerKey(key string) error {
	if strings.TrimSpace(key) != key || key == "" || len(key) > maximumMCPServerKeyLen || strings.IndexByte(key, 0) >= 0 {
		return fmt.Errorf("auth: MCP server key is invalid")
	}
	return nil
}

func validateMCPOAuthSession(session MCPOAuthSession) error {
	values := []string{
		session.Config.ClientID, session.Config.ClientSecret, session.Config.RedirectURL,
		session.Config.Endpoint.AuthURL, session.Config.Endpoint.TokenURL, session.Issuer,
		session.Token.AccessToken, session.Token.RefreshToken, session.Token.TokenType,
	}
	for _, value := range values {
		if len(value) > maximumOAuthValueLen || strings.IndexByte(value, 0) >= 0 {
			return fmt.Errorf("auth: MCP OAuth credential is oversized or malformed")
		}
	}
	if session.Config.ClientID == "" || session.Config.RedirectURL == "" || session.Config.Endpoint.AuthURL == "" || session.Config.Endpoint.TokenURL == "" || session.Token.AccessToken == "" {
		return fmt.Errorf("auth: MCP OAuth credential is missing required fields")
	}
	urls := []string{session.Config.RedirectURL, session.Config.Endpoint.AuthURL, session.Config.Endpoint.TokenURL}
	if session.Issuer != "" {
		urls = append(urls, session.Issuer)
	}
	for _, raw := range urls {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("auth: MCP OAuth credential contains an invalid URL")
		}
	}
	if len(session.Config.Scopes) > maximumOAuthScopes {
		return fmt.Errorf("auth: MCP OAuth credential has too many scopes")
	}
	for _, scope := range session.Config.Scopes {
		if scope == "" || len(scope) > 1024 || strings.IndexByte(scope, 0) >= 0 {
			return fmt.Errorf("auth: MCP OAuth credential contains an invalid scope")
		}
	}
	return nil
}

func encodeMCPOAuthEntry(session MCPOAuthSession, revision string) mcpOAuthEntry {
	return mcpOAuthEntry{
		Type: mcpOAuthEntryType, Revision: revision,
		ClientID: session.Config.ClientID, ClientSecret: session.Config.ClientSecret,
		RedirectURL: session.Config.RedirectURL, Scopes: append([]string(nil), session.Config.Scopes...),
		AuthURL: session.Config.Endpoint.AuthURL, TokenURL: session.Config.Endpoint.TokenURL,
		AuthStyle: session.Config.Endpoint.AuthStyle, Issuer: session.Issuer,
		AccessToken: session.Token.AccessToken, TokenType: session.Token.TokenType,
		RefreshToken: session.Token.RefreshToken, Expiry: session.Token.Expiry,
	}
}

func decodeMCPOAuthEntry(entry mcpOAuthEntry) (MCPOAuthSession, error) {
	if entry.Type != mcpOAuthEntryType {
		return MCPOAuthSession{}, fmt.Errorf("auth: MCP OAuth credential is missing or malformed")
	}
	if err := validateMCPRevision(entry.Revision); err != nil {
		return MCPOAuthSession{}, err
	}
	session := MCPOAuthSession{
		Config: oauth2.Config{
			ClientID: entry.ClientID, ClientSecret: entry.ClientSecret,
			Endpoint:    oauth2.Endpoint{AuthURL: entry.AuthURL, TokenURL: entry.TokenURL, AuthStyle: entry.AuthStyle},
			RedirectURL: entry.RedirectURL, Scopes: append([]string(nil), entry.Scopes...),
		},
		Token:  oauth2.Token{AccessToken: entry.AccessToken, TokenType: entry.TokenType, RefreshToken: entry.RefreshToken, Expiry: entry.Expiry},
		Issuer: entry.Issuer,
	}
	if err := validateMCPOAuthSession(session); err != nil {
		return MCPOAuthSession{}, err
	}
	return session, nil
}

func currentMCPRevision(entries authEntries, serverKey string) (string, error) {
	raw, ok := entries[serverKey]
	if !ok {
		return "", nil
	}
	var entry mcpOAuthEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return "", fmt.Errorf("auth: decode MCP OAuth credential: %w", err)
	}
	if entry.Type != mcpOAuthEntryType && entry.Type != mcpOAuthTombstoneType {
		return "", fmt.Errorf("auth: MCP OAuth credential is missing or malformed")
	}
	if err := validateMCPRevision(entry.Revision); err != nil {
		return "", err
	}
	return entry.Revision, nil
}

func validateMCPRevision(revision string) error {
	if len(revision) != 32 {
		return fmt.Errorf("auth: MCP OAuth credential revision is malformed")
	}
	if _, err := hex.DecodeString(revision); err != nil {
		return fmt.Errorf("auth: MCP OAuth credential revision is malformed")
	}
	return nil
}

func randomRevision() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("auth: generate MCP OAuth revision: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}
