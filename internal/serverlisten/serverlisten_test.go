package serverlisten

import (
	"net"
	"testing"
)

func TestParseAcceptsLoopbackAndUnspecifiedAddresses(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		canonical string
		network   string
		exposed   bool
	}{
		"127.0.0.1:0":    {canonical: "127.0.0.1:0", network: "tcp4"},
		"127.0.0.2:7777": {canonical: "127.0.0.2:7777", network: "tcp4"},
		"[::1]:7777":     {canonical: "[::1]:7777", network: "tcp6"},
		"0.0.0.0:7777":   {canonical: "0.0.0.0:7777", network: "tcp4", exposed: true},
		"[::]:7777":      {canonical: "[::]:7777", network: "tcp6", exposed: true},
	}
	for input, want := range cases {
		address, err := Parse(input)
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", input, err)
		}
		if address.String() != want.canonical || address.Network() != want.network || address.Exposed() != want.exposed {
			t.Fatalf("Parse(%q) = %s %s exposed=%v; want %s %s exposed=%v", input,
				address.String(), address.Network(), address.Exposed(), want.canonical, want.network, want.exposed)
		}
	}
}

func TestParseRejectsUnreachableOrAmbiguousAddresses(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		"",
		"7777",
		"localhost:7777",
		"192.168.1.10:7777",
		"100.64.0.1:7777",
		"[fe80::1%en0]:7777",
		"127.0.0.1:65536",
		"127.0.0.1:-1",
		"127.0.0.1:http",
	} {
		if _, err := Parse(input); err == nil {
			t.Errorf("Parse(%q) accepted an invalid listen address", input)
		}
	}
}

func TestLoopbackURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		listen string
		bound  net.Addr
		want   string
	}{
		{listen: "127.0.0.1:0", bound: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 51234}, want: "http://127.0.0.1:51234"},
		{listen: "0.0.0.0:7777", bound: &net.TCPAddr{IP: net.IPv4zero, Port: 7777}, want: "http://127.0.0.1:7777"},
		{listen: "[::]:7777", bound: &net.TCPAddr{IP: net.IPv6unspecified, Port: 7777}, want: "http://[::1]:7777"},
		{listen: "[::1]:0", bound: &net.TCPAddr{IP: net.IPv6loopback, Port: 4000}, want: "http://[::1]:4000"},
	}
	for _, test := range cases {
		got, err := MustParse(test.listen).LoopbackURL(test.bound)
		if err != nil {
			t.Fatalf("LoopbackURL(%s) error = %v", test.listen, err)
		}
		if got != test.want {
			t.Fatalf("LoopbackURL(%s) = %q, want %q", test.listen, got, test.want)
		}
	}
}

func TestServesMatchesPublishedDaemons(t *testing.T) {
	t.Parallel()
	cases := []struct {
		listen, published string
		want              bool
	}{
		{listen: "127.0.0.1:0", published: "http://127.0.0.1:51234", want: true},
		{listen: "127.0.0.1:7777", published: "http://127.0.0.1:7777", want: true},
		{listen: "127.0.0.1:7777", published: "http://127.0.0.1:51234", want: false},
		{listen: "0.0.0.0:7777", published: "http://127.0.0.1:7777", want: true},
		{listen: "0.0.0.0:7777", published: "http://[::1]:7777", want: false},
		{listen: "[::1]:7777", published: "http://127.0.0.1:7777", want: false},
		{listen: "127.0.0.1:7777", published: "not a url", want: false},
	}
	for _, test := range cases {
		if got := MustParse(test.listen).Serves(test.published); got != test.want {
			t.Errorf("%s serves %s = %v, want %v", test.listen, test.published, got, test.want)
		}
	}
}

func TestListenBindsTheAddress(t *testing.T) {
	t.Parallel()
	address := MustParse(Default)
	listener, err := address.Listen()
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()
	published, err := address.LoopbackURL(listener.Addr())
	if err != nil {
		t.Fatal(err)
	}
	if !address.Serves(published) {
		t.Fatalf("%s does not serve its own publication %s", address, published)
	}
}
