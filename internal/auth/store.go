// Package auth owns Kit's durable provider credential file.
package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/akonwi/kit/internal/droids"
	"github.com/akonwi/kit/internal/securefs"
	"github.com/gofrs/flock"
)

const (
	// OpenAIProviderID is the canonical auth-file key for the OpenAI API.
	OpenAIProviderID = "openai"
	// AnthropicProviderID is the canonical auth-file key for the Anthropic API.
	AnthropicProviderID = "anthropic"
	// OpenAICodexProviderID is the canonical auth-file key for ChatGPT Codex.
	OpenAICodexProviderID = "openai-codex"
	maximumAuthFileBytes  = 1 << 20
	maximumAPIKeyBytes    = 64 << 10
	lockPollInterval      = 25 * time.Millisecond
)

// ErrCredentialsChanged indicates that a refresh attempted to replace a newer
// login, logout, or refresh generation.
var ErrCredentialsChanged = droids.ErrOpenAICodexCredentialsChanged

// Store serializes access to Kit's shared JSON credential document.
type Store struct {
	path     string
	lockPath string
}

// CredentialInfo identifies one saved provider credential without exposing
// secret material.
type CredentialInfo struct {
	ProviderID string
	Type       string
}

// APIKeyRecord is one stored API-key credential generation.
type APIKeyRecord struct {
	APIKey   string
	Revision string
}

// NewStore creates a credential store at path without touching the filesystem.
func NewStore(path string) *Store {
	return &Store{path: path, lockPath: path + ".lock"}
}

var (
	_ droids.OpenAICodexCredentialStore = (*Store)(nil)
	_ droids.AnthropicCredentialStore   = (*Store)(nil)
)

// LoadOpenAICodexCredentials loads the current Codex credential generation. A
// missing entry has zero credentials and an empty revision.
func (s *Store) LoadOpenAICodexCredentials(ctx context.Context) (droids.OpenAICodexCredentialRecord, error) {
	var record droids.OpenAICodexCredentialRecord
	err := s.withLock(ctx, func() error {
		entries, err := readAuthEntries(s.path)
		if err != nil {
			return err
		}
		raw, ok := entries[OpenAICodexProviderID]
		if !ok {
			return nil
		}
		record.Credentials, err = decodeOpenAICodexCredential(raw)
		if err != nil {
			return err
		}
		record.Revision, err = decodeOpenAICodexRevision(raw)
		return err
	})
	return record, err
}

// SaveOpenAICodexCredentials atomically saves credentials refreshed by the
// provider if expectedRevision is still current. A login, logout, or another
// refresh causes ErrCredentialsChanged instead of a stale write.
func (s *Store) SaveOpenAICodexCredentials(
	ctx context.Context,
	expectedRevision string,
	credentials droids.OpenAICodexCredentials,
) (string, error) {
	if err := validateOpenAICodexCredential(credentials); err != nil {
		return "", err
	}
	if expectedRevision == "" {
		return "", ErrCredentialsChanged
	}
	var nextRevision string
	err := s.withLock(ctx, func() error {
		entries, err := readAuthEntries(s.path)
		if err != nil {
			return err
		}
		currentRevision := ""
		var currentCredentials droids.OpenAICodexCredentials
		if raw, ok := entries[OpenAICodexProviderID]; ok {
			currentCredentials, err = decodeOpenAICodexCredential(raw)
			if err != nil {
				return err
			}
			currentRevision, err = decodeOpenAICodexRevision(raw)
			if err != nil {
				return err
			}
		}
		if currentRevision != expectedRevision ||
			(currentCredentials.AccountID != "" && credentials.AccountID != "" && currentCredentials.AccountID != credentials.AccountID) {
			return ErrCredentialsChanged
		}
		nextRevision, err = newCredentialRevision()
		if err != nil {
			return err
		}
		entry, err := encodeOpenAICodexCredential(credentials, nextRevision)
		if err != nil {
			return err
		}
		entries[OpenAICodexProviderID] = entry
		return writeAuthEntries(s.path, entries)
	})
	return nextRevision, err
}

// ReplaceOpenAICodexCredentials atomically installs a freshly authenticated
// credential, including an intentional account change and a new generation.
func (s *Store) ReplaceOpenAICodexCredentials(ctx context.Context, credentials droids.OpenAICodexCredentials) error {
	if err := validateOpenAICodexCredential(credentials); err != nil {
		return err
	}
	return s.withLock(ctx, func() error {
		entries, err := readAuthEntries(s.path)
		if err != nil {
			return err
		}
		revision, err := newCredentialRevision()
		if err != nil {
			return err
		}
		entry, err := encodeOpenAICodexCredential(credentials, revision)
		if err != nil {
			return err
		}
		entries[OpenAICodexProviderID] = entry
		return writeAuthEntries(s.path, entries)
	})
}

// LoadAnthropicCredentials loads either the saved API key or Claude
// subscription OAuth generation. A missing entry returns a zero record.
func (s *Store) LoadAnthropicCredentials(ctx context.Context) (droids.AnthropicCredentialRecord, error) {
	var record droids.AnthropicCredentialRecord
	err := s.withLock(ctx, func() error {
		entries, err := readAuthEntries(s.path)
		if err != nil {
			return err
		}
		raw, ok := entries[AnthropicProviderID]
		if !ok {
			return nil
		}
		var header struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &header); err != nil {
			return fmt.Errorf("auth: decode Anthropic credential: %w", err)
		}
		switch header.Type {
		case "api_key":
			apiKey, err := decodeAPIKeyCredential(raw)
			if err != nil {
				return err
			}
			record.Credentials.APIKey, record.Revision = apiKey.APIKey, apiKey.Revision
		case "oauth":
			credentials, revision, err := decodeAnthropicOAuthCredential(raw)
			if err != nil {
				return err
			}
			record.Credentials, record.Revision = credentials, revision
		default:
			return fmt.Errorf("auth: Anthropic credential type %q is unsupported", header.Type)
		}
		return nil
	})
	return record, err
}

// SaveAnthropicCredentials atomically saves refreshed OAuth credentials when
// expectedRevision is still current.
func (s *Store) SaveAnthropicCredentials(ctx context.Context, expectedRevision string, credentials droids.AnthropicCredentials) (string, error) {
	if err := validateAnthropicOAuthCredential(credentials); err != nil {
		return "", err
	}
	if expectedRevision == "" {
		return "", droids.ErrAnthropicCredentialsChanged
	}
	var nextRevision string
	err := s.withLock(ctx, func() error {
		entries, err := readAuthEntries(s.path)
		if err != nil {
			return err
		}
		raw, ok := entries[AnthropicProviderID]
		if !ok {
			return droids.ErrAnthropicCredentialsChanged
		}
		_, currentRevision, err := decodeAnthropicOAuthCredential(raw)
		if err != nil || currentRevision != expectedRevision {
			return droids.ErrAnthropicCredentialsChanged
		}
		nextRevision, err = newCredentialRevision()
		if err != nil {
			return err
		}
		entry, err := encodeAnthropicOAuthCredential(credentials, nextRevision)
		if err != nil {
			return err
		}
		entries[AnthropicProviderID] = entry
		return writeAuthEntries(s.path, entries)
	})
	return nextRevision, err
}

// ReplaceAnthropicOAuthCredentials installs a freshly authenticated Claude
// subscription credential as a new generation.
func (s *Store) ReplaceAnthropicOAuthCredentials(ctx context.Context, credentials droids.AnthropicCredentials) error {
	if err := validateAnthropicOAuthCredential(credentials); err != nil {
		return err
	}
	return s.withLock(ctx, func() error {
		entries, err := readAuthEntries(s.path)
		if err != nil {
			return err
		}
		revision, err := newCredentialRevision()
		if err != nil {
			return err
		}
		entry, err := encodeAnthropicOAuthCredential(credentials, revision)
		if err != nil {
			return err
		}
		entries[AnthropicProviderID] = entry
		return writeAuthEntries(s.path, entries)
	})
}

// LoadAPIKey loads the current API-key credential generation. A missing entry
// returns a zero record.
func (s *Store) LoadAPIKey(ctx context.Context, providerID string) (APIKeyRecord, error) {
	if !supportedAPIKeyProvider(providerID) {
		return APIKeyRecord{}, fmt.Errorf("auth: API-key provider is unsupported")
	}
	var record APIKeyRecord
	err := s.withLock(ctx, func() error {
		entries, err := readAuthEntries(s.path)
		if err != nil {
			return err
		}
		raw, ok := entries[providerID]
		if !ok {
			return nil
		}
		record, err = decodeAPIKeyCredential(raw)
		return err
	})
	return record, err
}

// ReplaceAPIKey atomically installs an API key as a new credential generation.
func (s *Store) ReplaceAPIKey(ctx context.Context, providerID, apiKey string) error {
	if !supportedAPIKeyProvider(providerID) {
		return fmt.Errorf("auth: API-key provider is unsupported")
	}
	if err := validateAPIKey(apiKey); err != nil {
		return err
	}
	return s.withLock(ctx, func() error {
		entries, err := readAuthEntries(s.path)
		if err != nil {
			return err
		}
		revision, err := newCredentialRevision()
		if err != nil {
			return err
		}
		entry, err := encodeAPIKeyCredential(apiKey, revision)
		if err != nil {
			return err
		}
		entries[providerID] = entry
		return writeAuthEntries(s.path, entries)
	})
}

// Delete removes a provider's credential while preserving all other entries.
func (s *Store) Delete(ctx context.Context, providerID string) error {
	if !validAuthMetadata(providerID, 256) {
		return fmt.Errorf("auth: provider id is missing or malformed")
	}
	return s.withLock(ctx, func() error {
		entries, err := readAuthEntries(s.path)
		if err != nil {
			return err
		}
		if _, exists := entries[providerID]; !exists {
			return nil
		}
		delete(entries, providerID)
		return writeAuthEntries(s.path, entries)
	})
}

// List returns non-secret metadata for every saved credential.
func (s *Store) List(ctx context.Context) ([]CredentialInfo, error) {
	var result []CredentialInfo
	err := s.withLock(ctx, func() error {
		entries, err := readAuthEntries(s.path)
		if err != nil {
			return err
		}
		result = make([]CredentialInfo, 0, len(entries))
		for providerID, raw := range entries {
			if !validAuthMetadata(providerID, 256) {
				return fmt.Errorf("auth: credential has a malformed provider id")
			}
			var header struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(raw, &header); err != nil {
				return fmt.Errorf("auth: decode credential for %q: %w", providerID, err)
			}
			if !validAuthMetadata(header.Type, 64) {
				return fmt.Errorf("auth: credential for %q has a malformed type", providerID)
			}
			result = append(result, CredentialInfo{ProviderID: providerID, Type: header.Type})
		}
		sort.Slice(result, func(i, j int) bool { return result[i].ProviderID < result[j].ProviderID })
		return nil
	})
	return result, err
}

func (s *Store) withLock(ctx context.Context, operation func() error) (result error) {
	if s == nil || s.path == "" {
		return fmt.Errorf("auth: credential path is empty")
	}
	if !filepath.IsAbs(s.path) {
		return fmt.Errorf("auth: credential path must be absolute")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	directory := filepath.Dir(s.path)
	if err := securefs.MakePrivateDir(directory); err != nil {
		return fmt.Errorf("auth: protect credential directory: %w", err)
	}
	if err := prepareLockFile(s.lockPath); err != nil {
		return err
	}
	fileLock := flock.New(s.lockPath)
	locked, err := fileLock.TryLockContext(ctx, lockPollInterval)
	if err != nil {
		return fmt.Errorf("auth: acquire credential lock: %w", err)
	}
	if !locked {
		return fmt.Errorf("auth: credential lock was not acquired")
	}
	defer func() {
		if err := fileLock.Unlock(); err != nil {
			result = errors.Join(result, fmt.Errorf("auth: release credential lock: %w", err))
		}
	}()
	return operation()
}

func prepareLockFile(path string) error {
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("auth: credential lock path is not a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("auth: inspect credential lock: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("auth: create credential lock: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("auth: close credential lock: %w", err)
	}
	if err := securefs.ProtectFile(path); err != nil {
		return fmt.Errorf("auth: protect credential lock: %w", err)
	}
	return nil
}

type authEntries map[string]json.RawMessage

func readAuthEntries(path string) (authEntries, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return make(authEntries), nil
	}
	if err != nil {
		return nil, fmt.Errorf("auth: inspect credential file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("auth: credential path is not a regular file")
	}
	if info.Size() > maximumAuthFileBytes {
		return nil, fmt.Errorf("auth: credential file exceeds 1 MiB")
	}
	if err := securefs.ProtectFile(path); err != nil {
		return nil, fmt.Errorf("auth: protect credential file: %w", err)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("auth: open credential file: %w", err)
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, maximumAuthFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("auth: read credential file: %w", err)
	}
	if len(body) > maximumAuthFileBytes {
		return nil, fmt.Errorf("auth: credential file exceeds 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	var entries authEntries
	if err := decoder.Decode(&entries); err != nil {
		return nil, fmt.Errorf("auth: decode credential file: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("auth: credential file contains multiple JSON values")
		}
		return nil, fmt.Errorf("auth: decode credential file: %w", err)
	}
	if entries == nil {
		entries = make(authEntries)
	}
	return entries, nil
}

func writeAuthEntries(path string, entries authEntries) error {
	body, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("auth: encode credential file: %w", err)
	}
	body = append(body, '\n')
	if len(body) > maximumAuthFileBytes {
		return fmt.Errorf("auth: credential file exceeds 1 MiB")
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".auth-write-*")
	if err != nil {
		return fmt.Errorf("auth: create temporary credential file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	fail := func(operation string, err error) error {
		_ = temporary.Close()
		return fmt.Errorf("auth: %s: %w", operation, err)
	}
	if err := securefs.ProtectFile(temporaryPath); err != nil {
		return fail("protect temporary credential file", err)
	}
	if _, err := temporary.Write(body); err != nil {
		return fail("write temporary credential file", err)
	}
	if err := temporary.Sync(); err != nil {
		return fail("sync temporary credential file", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("auth: close temporary credential file: %w", err)
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("auth: credential path is not a regular file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("auth: inspect credential destination: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("auth: publish credential file: %w", err)
	}
	if err := syncDirectory(directory); err != nil {
		return err
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("auth: open credential directory for sync: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("auth: sync credential directory: %w", err)
	}
	return nil
}

type apiKeyEntry struct {
	Type     string `json:"type"`
	APIKey   string `json:"apiKey"`
	Revision string `json:"revision"`
}

func decodeAPIKeyCredential(raw json.RawMessage) (APIKeyRecord, error) {
	var entry apiKeyEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return APIKeyRecord{}, fmt.Errorf("auth: decode API-key credential: %w", err)
	}
	if entry.Type != "api_key" {
		return APIKeyRecord{}, fmt.Errorf("auth: credential type is unsupported")
	}
	if err := validateAPIKey(entry.APIKey); err != nil {
		return APIKeyRecord{}, err
	}
	if !validCredentialRevision(entry.Revision) {
		return APIKeyRecord{}, fmt.Errorf("auth: API-key credential revision is missing or malformed")
	}
	return APIKeyRecord{APIKey: entry.APIKey, Revision: entry.Revision}, nil
}

func encodeAPIKeyCredential(apiKey, revision string) (json.RawMessage, error) {
	if err := validateAPIKey(apiKey); err != nil {
		return nil, err
	}
	if !validCredentialRevision(revision) {
		return nil, fmt.Errorf("auth: API-key credential revision is missing or malformed")
	}
	body, err := json.Marshal(apiKeyEntry{Type: "api_key", APIKey: apiKey, Revision: revision})
	if err != nil {
		return nil, fmt.Errorf("auth: encode API-key credential: %w", err)
	}
	return body, nil
}

func supportedAPIKeyProvider(providerID string) bool {
	return providerID == OpenAIProviderID || providerID == AnthropicProviderID
}

func validateAPIKey(apiKey string) error {
	if apiKey == "" || len(apiKey) > maximumAPIKeyBytes || malformedSecret(apiKey) {
		return fmt.Errorf("auth: API key is missing or malformed")
	}
	for _, character := range apiKey {
		if character < 0x21 || character == 0x7f {
			return fmt.Errorf("auth: API key is missing or malformed")
		}
	}
	return nil
}

type anthropicOAuthEntry struct {
	Type         string `json:"type"`
	AccessToken  string `json:"access"`
	RefreshToken string `json:"refresh"`
	ExpiresAt    int64  `json:"expires"`
	Revision     string `json:"revision"`
}

func decodeAnthropicOAuthCredential(raw json.RawMessage) (droids.AnthropicCredentials, string, error) {
	var entry anthropicOAuthEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return droids.AnthropicCredentials{}, "", fmt.Errorf("auth: decode Anthropic OAuth credential: %w", err)
	}
	if entry.Type != "oauth" {
		return droids.AnthropicCredentials{}, "", fmt.Errorf("auth: Anthropic credential type %q is unsupported", entry.Type)
	}
	credentials := droids.AnthropicCredentials{AccessToken: entry.AccessToken, RefreshToken: entry.RefreshToken}
	if entry.ExpiresAt > 0 {
		credentials.ExpiresAt = time.UnixMilli(entry.ExpiresAt).UTC()
	}
	if err := validateAnthropicOAuthCredential(credentials); err != nil {
		return droids.AnthropicCredentials{}, "", err
	}
	if !validCredentialRevision(entry.Revision) {
		return droids.AnthropicCredentials{}, "", fmt.Errorf("auth: Anthropic OAuth credential revision is missing or malformed")
	}
	return credentials, entry.Revision, nil
}

func encodeAnthropicOAuthCredential(credentials droids.AnthropicCredentials, revision string) (json.RawMessage, error) {
	if err := validateAnthropicOAuthCredential(credentials); err != nil {
		return nil, err
	}
	if !validCredentialRevision(revision) {
		return nil, fmt.Errorf("auth: Anthropic OAuth credential revision is missing or malformed")
	}
	return json.Marshal(anthropicOAuthEntry{
		Type: "oauth", AccessToken: credentials.AccessToken, RefreshToken: credentials.RefreshToken,
		ExpiresAt: credentials.ExpiresAt.UnixMilli(), Revision: revision,
	})
}

func validateAnthropicOAuthCredential(credentials droids.AnthropicCredentials) error {
	if credentials.APIKey != "" || credentials.AccessToken == "" || credentials.RefreshToken == "" ||
		malformedSecret(credentials.AccessToken) || malformedSecret(credentials.RefreshToken) {
		return fmt.Errorf("auth: Anthropic OAuth credential is missing or malformed")
	}
	if credentials.ExpiresAt.IsZero() || credentials.ExpiresAt.UnixMilli() <= 0 {
		return fmt.Errorf("auth: Anthropic OAuth credential contains an invalid expiry")
	}
	return nil
}

type openAICodexEntry struct {
	Type         string `json:"type"`
	AccessToken  string `json:"access,omitempty"`
	RefreshToken string `json:"refresh,omitempty"`
	ExpiresAt    int64  `json:"expires,omitempty"`
	AccountID    string `json:"accountId,omitempty"`
	IDToken      string `json:"idToken,omitempty"`
	FedRAMP      bool   `json:"fedRAMP,omitempty"`
	Revision     string `json:"revision,omitempty"`
}

func decodeOpenAICodexCredential(raw json.RawMessage) (droids.OpenAICodexCredentials, error) {
	var entry openAICodexEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return droids.OpenAICodexCredentials{}, fmt.Errorf("auth: decode OpenAI Codex credential: %w", err)
	}
	if entry.Type != "oauth" {
		return droids.OpenAICodexCredentials{}, fmt.Errorf("auth: OpenAI Codex credential type %q is unsupported", entry.Type)
	}
	credentials := droids.OpenAICodexCredentials{
		AccessToken: entry.AccessToken, RefreshToken: entry.RefreshToken,
		IDToken: entry.IDToken, AccountID: entry.AccountID, FedRAMP: entry.FedRAMP,
	}
	if entry.ExpiresAt < 0 {
		return droids.OpenAICodexCredentials{}, fmt.Errorf("auth: OpenAI Codex credential contains an invalid expiry")
	}
	if entry.ExpiresAt > 0 {
		credentials.ExpiresAt = time.UnixMilli(entry.ExpiresAt).UTC()
	}
	if err := validateOpenAICodexCredential(credentials); err != nil {
		return droids.OpenAICodexCredentials{}, err
	}
	return credentials, nil
}

func decodeOpenAICodexRevision(raw json.RawMessage) (string, error) {
	var entry struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		return "", fmt.Errorf("auth: decode OpenAI Codex credential revision: %w", err)
	}
	if entry.Revision != "" {
		if !validCredentialRevision(entry.Revision) {
			return "", fmt.Errorf("auth: OpenAI Codex credential has a malformed revision")
		}
		return entry.Revision, nil
	}
	// Existing Kit auth files predate revisions. Hash the canonical known
	// credential fields so harmless JSON reformatting does not manufacture a
	// new logical generation during refresh.
	var legacy openAICodexEntry
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return "", fmt.Errorf("auth: decode legacy OpenAI Codex revision: %w", err)
	}
	legacy.Revision = ""
	canonical, err := json.Marshal(legacy)
	if err != nil {
		return "", fmt.Errorf("auth: encode legacy OpenAI Codex revision: %w", err)
	}
	hash := sha256.Sum256(canonical)
	return "legacy:" + hex.EncodeToString(hash[:]), nil
}

func encodeOpenAICodexCredential(credentials droids.OpenAICodexCredentials, revision string) (json.RawMessage, error) {
	if err := validateOpenAICodexCredential(credentials); err != nil {
		return nil, err
	}
	if !validCredentialRevision(revision) {
		return nil, fmt.Errorf("auth: OpenAI Codex credential revision is missing or malformed")
	}
	expiresAt := int64(0)
	if !credentials.ExpiresAt.IsZero() {
		expiresAt = credentials.ExpiresAt.UnixMilli()
	}
	body, err := json.Marshal(openAICodexEntry{
		Type: "oauth", AccessToken: credentials.AccessToken,
		RefreshToken: credentials.RefreshToken, ExpiresAt: expiresAt,
		AccountID: credentials.AccountID, IDToken: credentials.IDToken,
		FedRAMP: credentials.FedRAMP, Revision: revision,
	})
	if err != nil {
		return nil, fmt.Errorf("auth: encode OpenAI Codex credential: %w", err)
	}
	return body, nil
}

func validateOpenAICodexCredential(credentials droids.OpenAICodexCredentials) error {
	if malformedSecret(credentials.AccessToken) || malformedSecret(credentials.RefreshToken) || malformedSecret(credentials.IDToken) {
		return fmt.Errorf("auth: OpenAI Codex credential contains a malformed token")
	}
	if credentials.AccessToken == "" && credentials.RefreshToken == "" {
		return fmt.Errorf("auth: OpenAI Codex credential has no access or refresh token")
	}
	if credentials.AccountID != strings.TrimSpace(credentials.AccountID) {
		return fmt.Errorf("auth: OpenAI Codex credential contains a malformed account id")
	}
	if !credentials.ExpiresAt.IsZero() && credentials.ExpiresAt.UnixMilli() <= 0 {
		return fmt.Errorf("auth: OpenAI Codex credential contains an invalid expiry")
	}
	return nil
}

func newCredentialRevision() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("auth: generate credential revision: %w", err)
	}
	return "v1:" + hex.EncodeToString(value), nil
}

func validCredentialRevision(revision string) bool {
	if revision == "" || revision != strings.TrimSpace(revision) || len(revision) > 128 {
		return false
	}
	for _, character := range revision {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}

func malformedSecret(value string) bool {
	return value != strings.TrimSpace(value)
}

func validAuthMetadata(value string, maximum int) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
