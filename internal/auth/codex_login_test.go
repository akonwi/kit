package auth

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akonwi/kit/internal/droids"
	codexauth "github.com/akonwi/kit/internal/droids/openaicodex"
)

func TestOpenAICodexDeviceLoginPersistsCompletedCredentials(t *testing.T) {
	t.Parallel()
	credentials := testCredentials("account")
	oauthClient := &fakeCodexDeviceOAuth{
		device: &codexauth.DeviceAuthorization{
			VerificationURI: "https://example.test/device", UserCode: "ABCD-EFGH",
			ExpiresAt: time.Now().Add(time.Minute),
		},
		credentials: credentials,
	}
	store := NewStore(filepath.Join(t.TempDir(), "auth.json"))
	login := newOpenAICodexDeviceLogin(oauthClient, store, nil)
	var instructions OpenAICodexDeviceInstructions
	if err := login.Login(context.Background(), func(value OpenAICodexDeviceInstructions) error {
		instructions = value
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if instructions.VerificationURI != oauthClient.device.VerificationURI || instructions.UserCode != oauthClient.device.UserCode {
		t.Fatalf("instructions = %#v", instructions)
	}
	record, err := store.LoadOpenAICodexCredentials(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if record.Credentials.AccessToken != credentials.AccessToken || record.Revision == "" {
		t.Fatalf("stored record = %#v", record)
	}
	if oauthClient.beginCalls != 1 || oauthClient.pollCalls != 1 {
		t.Fatalf("OAuth calls = begin %d, poll %d", oauthClient.beginCalls, oauthClient.pollCalls)
	}
}

func TestOpenAICodexDeviceLoginStopsWhenInstructionsCannotBeDelivered(t *testing.T) {
	t.Parallel()
	oauthClient := &fakeCodexDeviceOAuth{device: &codexauth.DeviceAuthorization{
		VerificationURI: "https://example.test/device", UserCode: "CODE",
		ExpiresAt: time.Now().Add(time.Minute),
	}}
	installer := &guardedCodexInstaller{}
	login := newOpenAICodexDeviceLogin(oauthClient, installer, nil)
	displayErr := errors.New("display failed")
	if err := login.Login(context.Background(), func(OpenAICodexDeviceInstructions) error { return displayErr }); !errors.Is(err, displayErr) {
		t.Fatalf("error = %v", err)
	}
	if oauthClient.pollCalls != 0 || installer.calls != 0 {
		t.Fatalf("poll calls = %d, installer calls = %d", oauthClient.pollCalls, installer.calls)
	}
}

func TestOpenAICodexDeviceLoginGuardsCredentialReplacement(t *testing.T) {
	t.Parallel()
	oauthClient := &fakeCodexDeviceOAuth{
		device: &codexauth.DeviceAuthorization{
			VerificationURI: "https://example.test/device", UserCode: "CODE",
			ExpiresAt: time.Now().Add(time.Minute),
		},
		credentials: testCredentials("account"),
	}
	held := false
	installer := &guardedCodexInstaller{held: &held}
	login := newOpenAICodexDeviceLogin(oauthClient, installer, func(context.Context) (func() error, error) {
		held = true
		return func() error {
			held = false
			return nil
		}, nil
	})
	if err := login.Login(context.Background(), func(OpenAICodexDeviceInstructions) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if held || installer.calls != 1 {
		t.Fatalf("held = %v, installer calls = %d", held, installer.calls)
	}
}

func TestOpenAICodexDeviceLoginReleasesGuardAfterSaveFailure(t *testing.T) {
	t.Parallel()
	oauthClient := &fakeCodexDeviceOAuth{
		device: &codexauth.DeviceAuthorization{
			VerificationURI: "https://example.test/device", UserCode: "CODE",
			ExpiresAt: time.Now().Add(time.Minute),
		},
		credentials: testCredentials("account"),
	}
	held := false
	installer := &guardedCodexInstaller{held: &held, err: errors.New("save failed")}
	login := newOpenAICodexDeviceLogin(oauthClient, installer, func(context.Context) (func() error, error) {
		held = true
		return func() error {
			held = false
			return nil
		}, nil
	})
	err := login.Login(context.Background(), func(OpenAICodexDeviceInstructions) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "save credentials") {
		t.Fatalf("error = %v", err)
	}
	if held {
		t.Fatal("save guard remained held")
	}
}

func TestOpenAICodexDeviceLoginRejectsIncompleteInstructions(t *testing.T) {
	t.Parallel()
	login := newOpenAICodexDeviceLogin(
		&fakeCodexDeviceOAuth{device: &codexauth.DeviceAuthorization{}},
		&guardedCodexInstaller{},
		nil,
	)
	if err := login.Login(context.Background(), func(OpenAICodexDeviceInstructions) error { return nil }); err == nil {
		t.Fatal("incomplete device instructions were accepted")
	}
}

func TestOpenAICodexDeviceLoginPropagatesCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	oauthClient := &fakeCodexDeviceOAuth{
		device: &codexauth.DeviceAuthorization{
			VerificationURI: "https://example.test/device", UserCode: "CODE",
			ExpiresAt: time.Now().Add(time.Minute),
		},
		poll: func(ctx context.Context) (codexauth.Credentials, error) {
			cancel()
			return codexauth.Credentials{}, ctx.Err()
		},
	}
	login := newOpenAICodexDeviceLogin(oauthClient, &guardedCodexInstaller{}, nil)
	if err := login.Login(ctx, func(OpenAICodexDeviceInstructions) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

type fakeCodexDeviceOAuth struct {
	device      *codexauth.DeviceAuthorization
	credentials codexauth.Credentials
	beginErr    error
	pollErr     error
	poll        func(context.Context) (codexauth.Credentials, error)
	beginCalls  int
	pollCalls   int
}

func (f *fakeCodexDeviceOAuth) BeginDeviceAuthorization(context.Context) (*codexauth.DeviceAuthorization, error) {
	f.beginCalls++
	return f.device, f.beginErr
}

func (f *fakeCodexDeviceOAuth) PollDeviceAuthorization(ctx context.Context, _ *codexauth.DeviceAuthorization) (codexauth.Credentials, error) {
	f.pollCalls++
	if f.poll != nil {
		return f.poll(ctx)
	}
	return f.credentials, f.pollErr
}

type guardedCodexInstaller struct {
	held  *bool
	err   error
	calls int
}

func (s *guardedCodexInstaller) ReplaceOpenAICodexCredentials(_ context.Context, _ droids.OpenAICodexCredentials) error {
	s.calls++
	if s.held != nil && !*s.held {
		return errors.New("save guard was not held")
	}
	return s.err
}
