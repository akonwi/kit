package pairing

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestCodesAreSingleUseAndExpire(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	registry := NewRegistry(func() time.Time { return now })
	code, expires, err := registry.Issue(CommandLifetime)
	if err != nil {
		t.Fatal(err)
	}
	if !ValidCode(code) || !expires.Equal(now.Add(CommandLifetime)) {
		t.Fatalf("Issue() = %q, %v", code, expires)
	}
	if err := registry.Redeem(code); err != nil {
		t.Fatalf("Redeem() error = %v", err)
	}
	if err := registry.Redeem(code); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("second Redeem() error = %v, want ErrInvalidCode", err)
	}

	expiring, _, _ := registry.Issue(CommandLifetime)
	now = now.Add(CommandLifetime)
	if err := registry.Redeem(expiring); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("expired Redeem() error = %v, want ErrInvalidCode", err)
	}
	for _, malformed := range []string{"", "short", code + "a", "ABCDEFGHIJKLMNOPQRSTUVWXYZ"} {
		if err := registry.Redeem(malformed); !errors.Is(err, ErrInvalidCode) {
			t.Errorf("Redeem(%q) error = %v", malformed, err)
		}
	}
}

func TestIssuingBeyondTheLimitForgetsTheOldestCodes(t *testing.T) {
	registry := NewRegistry(nil)
	oldest, _, _ := registry.Issue(time.Hour)
	var newest string
	for range MaxOutstanding {
		newest, _, _ = registry.Issue(time.Hour)
	}
	if err := registry.Redeem(oldest); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("oldest code error = %v, want ErrInvalidCode", err)
	}
	if err := registry.Redeem(newest); err != nil {
		t.Fatalf("newest code error = %v", err)
	}
}

func TestLinksRoundTrip(t *testing.T) {
	code := "abcdefghijklmnopqrstuvwxyz"
	if !ValidCode(code) {
		t.Fatalf("%q should be a valid code", code)
	}
	link := Link("https://kit.example.ts.net/", code)
	if link != "https://kit.example.ts.net/pair#abcdefghijklmnopqrstuvwxyz" {
		t.Fatalf("Link() = %q", link)
	}
	server, parsed, err := ParseLink(link)
	if err != nil || server != "https://kit.example.ts.net" || parsed != code {
		t.Fatalf("ParseLink() = %q, %q, %v", server, parsed, err)
	}
	for _, invalid := range []string{
		"kit.example/pair#" + code,
		"ftp://kit.example/pair#" + code,
		"https://kit.example/other#" + code,
		"https://kit.example/pair?x=1#" + code,
		"https://user@kit.example/pair#" + code,
		"https://kit.example/pair#short",
		"https://kit.example/pair",
	} {
		if _, _, err := ParseLink(invalid); err == nil {
			t.Errorf("ParseLink(%q) succeeded", invalid)
		}
	}
}

func TestServedURLs(t *testing.T) {
	got := ServedURLs(
		[]string{"kit.example.ts.net", "box.local", "fd7a::1"},
		[]string{"http://box.local:8080"},
		"http://127.0.0.1:7777",
		"https://kit.example.ts.net/", "https://proxy.example",
	)
	want := []string{
		"https://kit.example.ts.net", "http://box.local:8080", "https://[fd7a::1]",
		"https://proxy.example", "http://127.0.0.1:7777",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ServedURLs() = %q, want %q", got, want)
	}
}

func TestValidServedURL(t *testing.T) {
	for _, valid := range []string{"https://kit.example.ts.net", "http://127.0.0.1:7777", "https://[fd7a::1]", "http://box.local:8080"} {
		if !ValidServedURL(valid) {
			t.Errorf("ValidServedURL(%q) = false", valid)
		}
	}
	for _, invalid := range []string{
		"", "kit.example.ts.net", "ftp://kit.example", "https://", "https://kit.example/", "https://kit.example/pair",
		"https://kit.example?", "https://kit.example#", "https://user@kit.example", "HTTPS://kit.example", " https://kit.example",
	} {
		if ValidServedURL(invalid) {
			t.Errorf("ValidServedURL(%q) = true", invalid)
		}
	}
}
