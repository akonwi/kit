package serveraccess

import "testing"

func TestParseHost(t *testing.T) {
	t.Parallel()
	valid := map[string]string{
		"studio.tail3f2a.ts.net":  "studio.tail3f2a.ts.net",
		"Studio.Tail3f2a.TS.net.": "studio.tail3f2a.ts.net",
		"localhost":               "localhost",
		"192.168.1.10":            "192.168.1.10",
		"[fd7a::1]":               "fd7a::1",
		"::ffff:10.0.0.1":         "10.0.0.1",
	}
	for input, want := range valid {
		got, err := ParseHost(input)
		if err != nil || got != want {
			t.Errorf("ParseHost(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"", "studio.ts.net:443", "https://studio.ts.net", "bad_host", "-bad.example", "a..b", "fe80::1%en0"} {
		if got, err := ParseHost(input); err == nil {
			t.Errorf("ParseHost(%q) = %q, want an error", input, got)
		}
	}
}

func TestParseOrigin(t *testing.T) {
	t.Parallel()
	valid := map[string]string{
		"https://studio.tail3f2a.ts.net": "https://studio.tail3f2a.ts.net",
		"HTTPS://Studio.TS.net:8443":     "https://studio.ts.net:8443",
		"http://[FD7A::1]:7777":          "http://[fd7a::1]:7777",
	}
	for input, want := range valid {
		got, err := ParseOrigin(input)
		if err != nil || got != want {
			t.Errorf("ParseOrigin(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"", "studio.ts.net", "ftp://studio.ts.net", "https://studio.ts.net/", "https://user@studio.ts.net", "https://studio.ts.net?x=1", "https://bad_host"} {
		if got, err := ParseOrigin(input); err == nil {
			t.Errorf("ParseOrigin(%q) = %q, want an error", input, got)
		}
	}
}

func TestHostAllowed(t *testing.T) {
	t.Parallel()
	policy := NewPolicy([]string{"studio.tail3f2a.ts.net", "192.168.1.10"}, nil)
	cases := map[string]bool{
		"127.0.0.1:51234":            true,
		"127.0.0.1":                  true,
		"127.8.9.10:7777":            true,
		"[::1]:7777":                 true,
		"studio.tail3f2a.ts.net":     true,
		"STUDIO.tail3f2a.ts.net:443": true,
		"studio.tail3f2a.ts.net.":    true,
		"192.168.1.10:7777":          true,
		"localhost:7777":             false,
		"evil.example":               false,
		"192.168.1.11:7777":          false,
		"0.0.0.0:7777":               false,
		"":                           false,
	}
	for host, want := range cases {
		if got := policy.HostAllowed(host); got != want {
			t.Errorf("HostAllowed(%q) = %v, want %v", host, got, want)
		}
	}
	if (Policy{}).HostAllowed("studio.tail3f2a.ts.net") {
		t.Error("the zero policy allowed a hostname")
	}
}

func TestOriginAllowed(t *testing.T) {
	t.Parallel()
	policy := NewPolicy(nil, []string{"https://app.example.com"})
	cases := []struct {
		origin, host string
		want         bool
	}{
		{origin: "", host: "127.0.0.1:7777", want: true},
		{origin: "http://127.0.0.1:7777", host: "127.0.0.1:7777", want: true},
		{origin: "https://studio.ts.net", host: "studio.ts.net", want: true},
		{origin: "http://STUDIO.ts.net", host: "studio.ts.net", want: true},
		{origin: "https://app.example.com", host: "studio.ts.net", want: true},
		{origin: "https://APP.example.com", host: "studio.ts.net", want: true},
		{origin: "https://evil.example", host: "studio.ts.net", want: false},
		{origin: "http://127.0.0.1:8000", host: "127.0.0.1:7777", want: false},
		{origin: "null", host: "127.0.0.1:7777", want: false},
		{origin: "file://", host: "127.0.0.1:7777", want: false},
	}
	for _, test := range cases {
		if got := policy.OriginAllowed(test.origin, test.host); got != test.want {
			t.Errorf("OriginAllowed(%q, %q) = %v, want %v", test.origin, test.host, got, test.want)
		}
	}
}
