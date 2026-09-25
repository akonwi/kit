package theme

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestParseColorCompatibleSpellings(t *testing.T) {
	t.Parallel()

	tests := map[string]Color{
		"transparent": Color{},
		"#123":        {R: 0x11, G: 0x22, B: 0x33, A: 0xff},
		"#1234":       {R: 0x11, G: 0x22, B: 0x33, A: 0x44},
		"#123456":     {R: 0x12, G: 0x34, B: 0x56, A: 0xff},
		"#12345678":   {R: 0x12, G: 0x34, B: 0x56, A: 0x78},
		"#AbCdEf":     {R: 0xab, G: 0xcd, B: 0xef, A: 0xff},
	}
	for spelling, want := range tests {
		spelling, want := spelling, want
		t.Run(spelling, func(t *testing.T) {
			t.Parallel()
			got, err := ParseColor(spelling)
			if err != nil {
				t.Fatalf("ParseColor() error = %v", err)
			}
			if got != want {
				t.Fatalf("ParseColor() = %#v, want %#v", got, want)
			}
		})
	}
}

func TestParseColorRejectsUnsupportedSpellings(t *testing.T) {
	t.Parallel()

	for _, spelling := range []string{"", "red", "#12", "#12345", "#1234567", "#ggg", " transparent"} {
		if _, err := ParseColor(spelling); err == nil {
			t.Errorf("ParseColor(%q) unexpectedly succeeded", spelling)
		}
	}
}

func TestColorStringNormalizesCompatibleColors(t *testing.T) {
	t.Parallel()

	for spelling, want := range map[string]string{
		"transparent": "transparent",
		"#ABC":        "#aabbcc",
		"#abcd":       "#aabbccdd",
		"#010203":     "#010203",
		"#01020304":   "#01020304",
	} {
		color, err := ParseColor(spelling)
		if err != nil {
			t.Fatal(err)
		}
		if got := color.String(); got != want {
			t.Errorf("ParseColor(%q).String() = %q, want %q", spelling, got, want)
		}
	}
}

func TestParsePreservesUnknownRolesAndTopLevelFields(t *testing.T) {
	t.Parallel()

	definition, diagnostics, err := Parse([]byte(`{
		"tokens": {"bg": "#123", "futureRole": "#01020304"},
		"syntaxPalette": {"futureSyntax": "#abcdef"},
		"metadata": {"author": "Ada"}
	}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %v", diagnostics)
	}
	if definition.Tokens["futureRole"].String() != "#01020304" || definition.SyntaxPalette["futureSyntax"].String() != "#abcdef" {
		t.Fatalf("unknown roles were not preserved: %#v", definition)
	}
	var metadata map[string]string
	if err := json.Unmarshal(definition.Extra["metadata"], &metadata); err != nil || metadata["author"] != "Ada" {
		t.Fatalf("metadata = %#v, error = %v", metadata, err)
	}
}

func TestParseRestrictsFullyTransparentKnownRoles(t *testing.T) {
	t.Parallel()

	definition, diagnostics, err := Parse([]byte(`{
		"tokens": {
			"bgTransparent": "transparent",
			"textPrimary": "transparent",
			"textMuted": "#0000",
			"futureTransparent": "transparent"
		},
		"syntaxPalette": {"text": "#00000000"}
	}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if _, ok := definition.Tokens[TokenBackgroundTransparent]; !ok {
		t.Fatal("bgTransparent override was omitted")
	}
	if _, ok := definition.Tokens["futureTransparent"]; !ok {
		t.Fatal("unknown transparent role was not preserved")
	}
	if len(diagnostics) != 3 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestParseQuotesUnsafeDiagnosticRoleNames(t *testing.T) {
	t.Parallel()

	_, diagnostics, err := Parse([]byte("{\"tokens\":{\"bad\\n\\u001b[31m\":42}}"))
	if err != nil || len(diagnostics) != 1 {
		t.Fatalf("Parse() diagnostics = %#v, error = %v", diagnostics, err)
	}
	message := diagnostics[0].Error()
	if strings.ContainsRune(message, '\n') || strings.ContainsRune(message, '\x1b') {
		t.Fatalf("diagnostic contains raw terminal controls: %q", message)
	}
}

func TestParseOmitsInvalidOverridesIndividually(t *testing.T) {
	t.Parallel()

	definition, diagnostics, err := Parse([]byte(`{
		"tokens": {"bg": "#123456", "badType": 42, "badColor": "blue"},
		"syntaxPalette": {"text": "#abcdef"}
	}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got := definition.Tokens["bg"].String(); got != "#123456" {
		t.Fatalf("valid token = %q", got)
	}
	if got := definition.SyntaxPalette["text"].String(); got != "#abcdef" {
		t.Fatalf("valid syntax token = %q", got)
	}
	if _, exists := definition.Tokens["badType"]; exists {
		t.Fatal("non-string override was retained")
	}
	if _, exists := definition.Tokens["badColor"]; exists {
		t.Fatal("invalid color override was retained")
	}
	if len(diagnostics) != 2 || diagnostics[0].Name != "badColor" || diagnostics[1].Name != "badType" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestParseRejectsMalformedDocuments(t *testing.T) {
	t.Parallel()

	for _, input := range [][]byte{
		[]byte(`[]`),
		[]byte(`{} {}`),
		[]byte(`{"tokens": {}, "tokens": {}}`),
		[]byte(`{`),
		{0xff},
	} {
		if _, _, err := Parse(input); err == nil {
			t.Errorf("Parse(%q) unexpectedly succeeded", input)
		}
	}
}

func TestParseOmitsMalformedSectionWithoutLosingOtherSection(t *testing.T) {
	t.Parallel()

	definition, diagnostics, err := Parse([]byte(`{
		"tokens": [],
		"syntaxPalette": {"text": "#abcdef"}
	}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(diagnostics) != 1 || diagnostics[0].Section != "tokens" || diagnostics[0].Name != "" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if got := definition.SyntaxPalette["text"].String(); got != "#abcdef" {
		t.Fatalf("syntax text = %q", got)
	}
}

func TestParseReportsDuplicateRoleWithoutLosingOtherSection(t *testing.T) {
	t.Parallel()

	definition, diagnostics, err := Parse([]byte(`{
		"tokens": {"bg": "#000", "bg": "#fff"},
		"syntaxPalette": {"text": "#abcdef"}
	}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Error(), "duplicate key") {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if got := definition.SyntaxPalette["text"].String(); got != "#abcdef" {
		t.Fatalf("syntax text = %q", got)
	}
}

func TestResolveUsesFallbackForMissingAndInvalidRoles(t *testing.T) {
	t.Parallel()

	base := Resolved{
		Tokens: map[string]Color{
			"bg":      {R: 1, G: 2, B: 3, A: 0xff},
			"warning": {R: 4, G: 5, B: 6, A: 0xff},
		},
		SyntaxPalette: map[string]Color{"text": {R: 7, G: 8, B: 9, A: 0xff}},
	}
	definition, diagnostics, err := Parse([]byte(`{
		"tokens": {"bg": "#aabbcc", "warning": "invalid", "future": "#123"}
	}`))
	if err != nil || len(diagnostics) != 1 {
		t.Fatalf("Parse() diagnostics = %v, error = %v", diagnostics, err)
	}
	resolved := Resolve(base, definition)
	if got := resolved.Tokens["bg"].String(); got != "#aabbcc" {
		t.Errorf("bg = %q", got)
	}
	if resolved.Tokens["warning"] != base.Tokens["warning"] {
		t.Errorf("invalid warning did not retain fallback")
	}
	if resolved.SyntaxPalette["text"] != base.SyntaxPalette["text"] {
		t.Errorf("missing syntax text did not retain fallback")
	}
	if got := resolved.Tokens["future"].String(); got != "#112233" {
		t.Errorf("future = %q", got)
	}
	resolved.Tokens["bg"] = Color{}
	if base.Tokens["bg"] == (Color{}) {
		t.Fatal("Resolve mutated the base")
	}
}

func TestDiscoverReturnsSafeDeterministicThemeNames(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	for _, name := range []string{"zeta.json", "Alpha.json", "ignored.JSON", "notes.txt", "system.json"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(directory, "directory.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	names, err := Discover(directory)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if want := []string{"Alpha", "zeta"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("Discover() = %v, want %v", names, want)
	}
}

func TestDiscoverMissingDirectoryIsEmpty(t *testing.T) {
	t.Parallel()

	names, err := Discover(filepath.Join(t.TempDir(), "missing"))
	if err != nil || len(names) != 0 {
		t.Fatalf("Discover() = %v, %v", names, err)
	}
}

func TestLoadValidatesNameAndBoundsInput(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "cozy.json"), []byte(`{"tokens":{"bg":"#123"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	definition, diagnostics, err := Load(directory, "cozy")
	if err != nil || len(diagnostics) != 0 || definition.Tokens["bg"].String() != "#112233" {
		t.Fatalf("Load() = %#v, %v, %v", definition, diagnostics, err)
	}
	for _, name := range []string{"", "system", "../cozy", `..\\cozy`} {
		if _, _, err := Load(directory, name); err == nil {
			t.Errorf("Load(%q) unexpectedly succeeded", name)
		}
	}

	large := strings.Repeat(" ", MaxFileBytes+1)
	if err := os.WriteFile(filepath.Join(directory, "large.json"), []byte(large), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(directory, "large"); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("large Load() error = %v", err)
	}
}

func TestDiscoverAndLoadRejectFIFO(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	if err := unix.Mkfifo(filepath.Join(directory, "pipe.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	names, err := Discover(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 0 {
		t.Fatalf("Discover() = %v, want no non-regular entries", names)
	}
	if _, _, err := Load(directory, "pipe"); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadRejectsSymlinkOutsideThemeDirectory(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "escape.json")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(directory, "escape"); err == nil {
		t.Fatal("Load() followed a symlink outside the theme directory")
	}
}

func TestRoleRegistriesMatchCompatibilityContract(t *testing.T) {
	t.Parallel()

	wantTokens := []string{
		"bg", "bgSurface", "bgMuted", "bgAccent", "bgTransparent", "borderDefault",
		"borderFocused", "borderAccent", "borderDebug", "borderStatus", "composerBashBorder",
		"composerBashExcludedBorder", "textPrimary", "textSecondary", "textMuted",
		"textPlaceholder", "textDebug", "userText", "userTextFocused", "userBorder",
		"assistantText", "toolText", "reviewText", "errorText", "warningText", "subagentText",
		"debugLabel", "metaText", "attachmentText", "cursor", "pickerBg", "pickerBorder",
		"pickerFocusedBg", "pickerFocusedText", "pickerItemText", "pickerScrollThumb",
		"pickerScrollTrack", "scrollbarFg", "scrollbarBg", "panelText", "progressNormal",
		"progressWarning", "progressCritical", "toggleOn", "diffAddedBg", "diffRemovedBg",
		"diffAddedContentBg", "diffRemovedContentBg", "diffAddedLineNumberBg",
		"diffRemovedLineNumberBg", "diffCursorBg", "diffCursorGutterBg", "diffCursorAddedBg",
		"diffCursorRemovedBg",
	}
	wantSyntax := []string{
		"text", "heading", "bold", "italic", "link", "list", "quote", "codeInline",
		"codeBlock", "strikethrough", "conceal", "comment", "string", "escape", "number",
		"keyword", "keywordType", "function", "operator", "variable", "member", "builtin",
		"type", "punctuation", "tag", "tagAttribute", "tagDelimiter", "attribute", "label",
	}
	if got := TokenRoles(); !reflect.DeepEqual(got, wantTokens) {
		t.Fatalf("TokenRoles() = %v, want %v", got, wantTokens)
	}
	if got := SyntaxRoles(); !reflect.DeepEqual(got, wantSyntax) {
		t.Fatalf("SyntaxRoles() = %v, want %v", got, wantSyntax)
	}
	for _, role := range wantTokens {
		if !IsKnownToken(role) {
			t.Errorf("IsKnownToken(%q) = false", role)
		}
	}
	for _, role := range wantSyntax {
		if !IsKnownSyntaxRole(role) {
			t.Errorf("IsKnownSyntaxRole(%q) = false", role)
		}
	}
	if IsKnownToken("future") || IsKnownSyntaxRole("future") {
		t.Fatal("unknown role reported as known")
	}
	tokens := TokenRoles()
	tokens[0] = "mutated"
	if TokenRoles()[0] == "mutated" {
		t.Fatal("TokenRoles returned shared storage")
	}
}

func TestValidName(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"cozy", "solarized-light", "my theme", "a..b", `a\\b`} {
		if !ValidName(name) {
			t.Errorf("ValidName(%q) = false", name)
		}
	}
	for _, name := range []string{"", ".", "..", "../cozy", "a/b", "a\x00b", "line\nbreak", "bidi\u202ename"} {
		if ValidName(name) {
			t.Errorf("ValidName(%q) = true", name)
		}
	}
}
