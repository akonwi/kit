package droids

import (
	"strings"
	"testing"
)

func TestFileContentConstructors(t *testing.T) {
	file, err := NewFileURL("report.pdf", "application/pdf", "https://files.example/report.pdf?signature=abc")
	if err != nil {
		t.Fatal(err)
	}
	if file.Filename != "report.pdf" || file.MediaType != "application/pdf" || file.URL != "https://files.example/report.pdf?signature=abc" {
		t.Fatalf("file = %#v", file)
	}

	inline := NewFileData("notes.txt", "text/plain", []byte("hello"))
	if inline.URL != "data:text/plain;base64,aGVsbG8=" {
		t.Fatalf("data URL = %q", inline.URL)
	}
}

func TestImageContentConstructors(t *testing.T) {
	image, err := NewImageURL("image/png", "data:image/png;base64,AQID")
	if err != nil {
		t.Fatal(err)
	}
	if image.MediaType != "image/png" || image.URL != "data:image/png;base64,AQID" {
		t.Fatalf("image = %#v", image)
	}

	inline := NewImageData("image/png", []byte{1, 2, 3})
	if inline.URL != "data:image/png;base64,AQID" {
		t.Fatalf("data URL = %q", inline.URL)
	}
}

func TestContentConstructorsValidateMetadataAndDataURLs(t *testing.T) {
	if _, err := NewFileURL("", "application/pdf", "https://files.example/report.pdf"); err == nil || !strings.Contains(err.Error(), "filename") {
		t.Fatalf("empty filename error = %v", err)
	}
	if _, err := NewFileURL("report.pdf", "", "https://files.example/report.pdf"); err == nil || !strings.Contains(err.Error(), "media type") {
		t.Fatalf("empty media type error = %v", err)
	}
	if _, err := NewImageURL("text/plain", "https://files.example/image"); err == nil || !strings.Contains(err.Error(), "not an image") {
		t.Fatalf("non-image media type error = %v", err)
	}
	for _, mediaType := range []string{"document", "application/*", "*/*"} {
		if _, err := NewFileURL("file", mediaType, "https://files.example/file"); err == nil || !strings.Contains(err.Error(), "concrete type/subtype") {
			t.Fatalf("media type %q error = %v", mediaType, err)
		}
	}
	if _, err := NewFileURL("notes.txt", "text/plain", "data:text/plain,hello"); err == nil || !strings.Contains(err.Error(), "base64") {
		t.Fatalf("non-base64 data URL error = %v", err)
	}
	if _, err := NewFileURL("notes.txt", "text/plain", "data:text/plain;base64,***"); err == nil || !strings.Contains(err.Error(), "valid base64") {
		t.Fatalf("malformed base64 error = %v", err)
	}
	if _, err := NewFileURL("notes.txt", "text/plain", "data:application/pdf;base64,aGVsbG8="); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatched data media type error = %v", err)
	}
	if _, err := NewImageURL("image/png", "data:text/plain;base64,aGVsbG8="); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatched image media type error = %v", err)
	}
}

func TestContentURLConstructorsRejectUnsafeSources(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{name: "empty", url: "", want: "required"},
		{name: "relative", url: "/report.pdf", want: "scheme"},
		{name: "http", url: "http://files.example/report.pdf", want: "scheme"},
		{name: "missing host", url: "https:///report.pdf", want: "host"},
		{name: "credentials", url: "https://user:pass@files.example/report.pdf", want: "credentials"},
		{name: "empty data URL", url: "data:", want: "include data"},
		{name: "whitespace", url: " https://files.example/report.pdf", want: "whitespace"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewFileURL("report.pdf", "application/pdf", tt.url); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("NewFileURL() error = %v, want %q", err, tt.want)
			}
			if _, err := NewImageURL("image/png", tt.url); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("NewImageURL() error = %v, want %q", err, tt.want)
			}
		})
	}
}
