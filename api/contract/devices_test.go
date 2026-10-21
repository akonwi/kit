package contract

import (
	"strings"
	"testing"
)

func TestPairingCodeValidatesItsURLs(t *testing.T) {
	code := PairingCode{Code: strings.Repeat("a", 26), ExpiresAt: "2026-03-01T12:05:00Z"}
	if err := code.Validate(); err != nil {
		t.Fatalf("code without URLs: %v", err)
	}
	code.URLs = []string{"https://kit.example.ts.net", "http://127.0.0.1:7777"}
	if err := code.Validate(); err != nil {
		t.Fatalf("code with URLs: %v", err)
	}
	for name, urls := range map[string][]string{
		"path":      {"https://kit.example.ts.net/pair"},
		"scheme":    {"ftp://kit.example.ts.net"},
		"repeated":  {"https://kit.example.ts.net", "https://kit.example.ts.net"},
		"too many":  slicesOf("https://kit.example.ts.net", MaxPairingURLs+1),
		"blank":     {""},
		"user info": {"https://me@kit.example.ts.net"},
	} {
		code.URLs = urls
		if err := code.Validate(); err == nil {
			t.Errorf("%s: Validate() accepted %q", name, urls)
		}
	}
}

func slicesOf(prefix string, count int) []string {
	urls := make([]string, count)
	for index := range urls {
		urls[index] = prefix + ":" + string(rune('0'+index%10)) + string(rune('0'+index/10))
	}
	return urls
}
