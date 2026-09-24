package mcpconfig

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

var (
	envPrefixPattern = regexp.MustCompile(`\$env:([A-Za-z0-9_]+)`)
	envBracePattern  = regexp.MustCompile(`\$\{([A-Za-z0-9_]+)(:-([^}]*))?\}`)
)

// normalize resolves one merged entry into a validated Server. It returns false
// when the entry cannot describe a usable connection.
func (l *Loader) normalize(name string, entry *mergedServer, result *Result) (Server, bool) {
	file := entry.source
	raw := entry.raw
	expand := func(field, value string) string {
		return l.expand(value, file, name, field, result)
	}

	server := Server{Name: name, Source: file.Source, Path: file.Path}
	if raw.description != nil {
		description := *raw.description
		if utf8.RuneCountInString(description) > maxDescriptionRunes {
			result.addDiagnostic(diagnose(file, name, "description",
				fmt.Sprintf("truncated: must be at most %d characters", maxDescriptionRunes)))
			description = string([]rune(description)[:maxDescriptionRunes])
		}
		server.Description = description
	}
	if raw.disabled != nil {
		server.Disabled = *raw.disabled
	}

	hasCommand := raw.command != nil && strings.TrimSpace(*raw.command) != ""
	hasURL := raw.url != nil && strings.TrimSpace(*raw.url) != ""
	switch {
	case hasCommand && hasURL:
		// Deterministic tie-break: the higher-precedence file decides, and a
		// command defined in the same file wins.
		if entry.urlRank > entry.commandRank {
			hasCommand = false
		} else {
			hasURL = false
		}
		result.addDiagnostic(diagnose(file, name, "",
			fmt.Sprintf("defines both command and url; using the %s transport", transportFor(hasCommand))))
	case !hasCommand && !hasURL:
		result.addDiagnostic(diagnose(file, name, "",
			`ignored: set "command" for a stdio server or "url" for an HTTP server`))
		return Server{}, false
	}

	server.Auth = l.normalizeAuth(raw, file, name, result)

	if hasCommand {
		server.Transport = TransportStdio
		server.Command = expand("command", *raw.command)
		if strings.TrimSpace(server.Command) == "" {
			result.addDiagnostic(diagnose(file, name, "command", "ignored: resolved to an empty command"))
			return Server{}, false
		}
		if strings.ContainsRune(server.Command, 0) {
			result.addDiagnostic(diagnose(file, name, "command", "ignored: must not contain NUL"))
			return Server{}, false
		}
		server.Args = []string{}
		if raw.args != nil {
			for index, arg := range *raw.args {
				expanded := expand(fmt.Sprintf("args[%d]", index), arg)
				if strings.ContainsRune(expanded, 0) {
					result.addDiagnostic(diagnose(file, name, fmt.Sprintf("args[%d]", index), "ignored: must not contain NUL"))
					continue
				}
				server.Args = append(server.Args, expanded)
			}
		}
		server.Env = map[string]string{}
		for _, key := range sortedStringKeys(raw.env) {
			server.Env[key] = expand("env."+key, raw.env[key])
		}
		if raw.cwd != nil {
			expanded := expand("cwd", *raw.cwd)
			if expanded != "" && !filepath.IsAbs(expanded) {
				result.addDiagnostic(diagnose(file, name, "cwd", "ignored: must be an absolute path"))
			} else {
				server.Cwd = filepath.Clean(expanded)
			}
		}
		if server.Auth != nil && server.Auth.Kind == AuthOAuth {
			result.addDiagnostic(diagnose(file, name, "auth", `ignored: "oauth" applies to HTTP servers only`))
			server.Auth = nil
		}
		return server, true
	}

	server.Transport = TransportHTTP
	endpoint := expand("url", *raw.url)
	if strings.TrimSpace(endpoint) == "" {
		result.addDiagnostic(diagnose(file, name, "url", "ignored: resolved to an empty URL"))
		return Server{}, false
	}
	parsed, err := url.Parse(endpoint)
	switch {
	case err != nil:
		result.addDiagnostic(diagnose(file, name, "url", fmt.Sprintf("ignored: must be a valid URL: %v", err)))
		return Server{}, false
	case parsed.Scheme != "http" && parsed.Scheme != "https":
		result.addDiagnostic(diagnose(file, name, "url", fmt.Sprintf("ignored: must use http or https, got %q", parsed.Scheme)))
		return Server{}, false
	case parsed.Host == "":
		result.addDiagnostic(diagnose(file, name, "url", "ignored: must include a host"))
		return Server{}, false
	}
	server.URL = parsed.String()
	server.Headers = map[string]string{}
	for _, key := range sortedStringKeys(raw.headers) {
		server.Headers[key] = expand("headers."+key, raw.headers[key])
	}
	if server.Auth != nil && server.Auth.Kind == AuthBearer && server.Auth.BearerToken != "" {
		if !hasHeader(server.Headers, "Authorization") {
			server.Headers["Authorization"] = "Bearer " + server.Auth.BearerToken
		}
	}
	return server, true
}

func (l *Loader) normalizeAuth(raw rawServer, file File, name string, result *Result) *Auth {
	var token, tokenEnv string
	if raw.bearerToken != nil {
		token = l.expand(*raw.bearerToken, file, name, "bearerToken", result)
	}
	if raw.bearerEnv != nil {
		tokenEnv = strings.TrimSpace(*raw.bearerEnv)
	}
	kind := ""
	if raw.auth != nil {
		kind = strings.TrimSpace(*raw.auth)
	}
	switch AuthKind(kind) {
	case AuthOAuth:
		return &Auth{Kind: AuthOAuth}
	case AuthBearer:
		if token == "" && tokenEnv == "" {
			result.addDiagnostic(diagnose(file, name, "auth",
				`"bearer" requires "bearerToken" or "bearerTokenEnv"`))
		}
		return &Auth{Kind: AuthBearer, BearerToken: token, BearerTokenEnv: tokenEnv}
	case "":
		if token != "" || tokenEnv != "" {
			return &Auth{Kind: AuthBearer, BearerToken: token, BearerTokenEnv: tokenEnv}
		}
		return nil
	default:
		result.addDiagnostic(diagnose(file, name, "auth",
			fmt.Sprintf("ignored: must be %q or %q, got %q", AuthOAuth, AuthBearer, kind)))
		return nil
	}
}

// expand resolves $env:VAR, ${VAR}, ${VAR:-default}, and a leading ~ against
// the loader's environment. An unset variable without a default expands to the
// empty string and reports an actionable diagnostic.
func (l *Loader) expand(value string, file File, server, field string, result *Result) string {
	lookup := func(key string, fallback *string) string {
		if resolved, ok := l.lookupEnv(key); ok {
			return resolved
		}
		if fallback != nil {
			return *fallback
		}
		result.addDiagnostic(diagnose(file, server, field,
			fmt.Sprintf("environment variable %q is not set; expanded to an empty value", key)))
		return ""
	}

	expanded := envPrefixPattern.ReplaceAllStringFunc(value, func(match string) string {
		return lookup(envPrefixPattern.FindStringSubmatch(match)[1], nil)
	})
	expanded = envBracePattern.ReplaceAllStringFunc(expanded, func(match string) string {
		groups := envBracePattern.FindStringSubmatch(match)
		if groups[2] != "" {
			return lookup(groups[1], &groups[3])
		}
		return lookup(groups[1], nil)
	})

	if l.userHome == "" {
		return expanded
	}
	if expanded == "~" {
		return l.userHome
	}
	if strings.HasPrefix(expanded, "~/") {
		return filepath.Join(l.userHome, expanded[2:])
	}
	return expanded
}

func transportFor(command bool) Transport {
	if command {
		return TransportStdio
	}
	return TransportHTTP
}

func hasHeader(headers map[string]string, name string) bool {
	for key := range headers {
		if strings.EqualFold(key, name) {
			return true
		}
	}
	return false
}

func sortedStringKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
