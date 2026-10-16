package authbridge

import (
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestReceiveRedirectPassesTheRedirectOnceAndStops(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/mcp/callback", port)
	received := make(chan string, 2)
	receiver, err := ReceiveRedirect("https://auth.example.com/authorize?state=s1", redirectURI, func(value string) { received <- value })
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Stop()
	response, err := http.Get(redirectURI + "?code=c&state=s1")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	select {
	case got := <-received:
		if got != redirectURI+"?code=c&state=s1" {
			t.Fatalf("received %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("the redirect was not received")
	}
	receiver.Stop()
	receiver.Stop()
	if _, err := http.Get(redirectURI + "?code=c&state=s1"); err == nil {
		t.Fatal("still listening after Stop")
	}
}

func TestReceiveRedirectListensOnlyOnLoopback(t *testing.T) {
	_, err := ReceiveRedirect("https://auth.example.com/authorize?state=s1", "http://0.0.0.0:53173/mcp/callback", func(string) {})
	if err == nil || err.Error() != "the redirect is not a loopback HTTP address" {
		t.Fatalf("error = %v", err)
	}
}
