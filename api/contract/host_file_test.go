package contract

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHostFileReadRoundTripsEachKind(t *testing.T) {
	for _, read := range []HostFileRead{
		{Payload: HostTextFile{SessionID: "session_1", Path: "/etc/hosts", Size: 6, ModifiedAt: "2026-01-02T03:04:05Z", Encoding: "utf-8", Content: "a\nb\nc\n", ReturnedBytes: 6, ReturnedLines: 3}},
		{Payload: HostTextFile{SessionID: "session_1", Path: "/tmp/empty", Encoding: "utf-8"}},
		{Payload: HostImageFile{SessionID: "session_1", Path: "/tmp/a.png", Size: 3, MediaType: "image/png", Width: 1, Height: 1, Data: []byte{1, 2, 3}}},
	} {
		if err := read.Validate(); err != nil {
			t.Fatalf("validate %+v: %v", read, err)
		}
		encoded, err := json.Marshal(read)
		if err != nil {
			t.Fatal(err)
		}
		var decoded HostFileRead
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("decode %s: %v", encoded, err)
		}
		again, _ := json.Marshal(decoded)
		if string(again) != string(encoded) || decoded.Kind() != read.Kind() {
			t.Fatalf("round trip = %s, want %s", again, encoded)
		}
	}
}

func TestHostFileReadWireShapes(t *testing.T) {
	text, _ := json.Marshal(HostFileRead{Payload: HostTextFile{SessionID: "s", Path: "/a", Size: 2, Encoding: "utf-8", Content: "hi", ReturnedBytes: 2, ReturnedLines: 1}})
	if string(text) != `{"kind":"text","sessionId":"s","path":"/a","size":2,"encoding":"utf-8","content":"hi","returnedBytes":2,"returnedLines":1}` {
		t.Fatalf("text = %s", text)
	}
	image, _ := json.Marshal(HostFileRead{Payload: HostImageFile{SessionID: "s", Path: "/a.gif", Size: 2, MediaType: "image/gif", Width: 4, Height: 5, Data: []byte("hi")}})
	if string(image) != `{"kind":"image","sessionId":"s","path":"/a.gif","size":2,"mediaType":"image/gif","width":4,"height":5,"data":"aGk="}` {
		t.Fatalf("image = %s", image)
	}
}

func TestHostFileReadDecodingIsStrict(t *testing.T) {
	for _, body := range []string{
		`{"kind":"video","sessionId":"s","path":"/a"}`,
		`{"kind":"text","sessionId":"s","path":"/a","encoding":"utf-8","mediaType":"image/png"}`,
		`{"kind":"image","sessionId":"s","path":"/a","size":1,"mediaType":"image/png","width":1,"height":1}`,
		`{"sessionId":"s","path":"/a","encoding":"utf-8"}`,
	} {
		var read HostFileRead
		if err := json.Unmarshal([]byte(body), &read); err == nil {
			t.Fatalf("decoded %s", body)
		}
	}
}

func TestHostFileReadValidationRejectsInconsistentReads(t *testing.T) {
	valid := HostTextFile{SessionID: "s", Path: "/a", Size: 2, Encoding: "utf-8", Content: "hi", ReturnedBytes: 2, ReturnedLines: 1}
	for name, mutate := range map[string]func(*HostTextFile){
		"relative path":   func(t *HostTextFile) { t.Path = "a" },
		"unclean path":    func(t *HostTextFile) { t.Path = "/a/../b" },
		"home path":       func(t *HostTextFile) { t.Path = "~/a" },
		"no session":      func(t *HostTextFile) { t.SessionID = "" },
		"wrong encoding":  func(t *HostTextFile) { t.Encoding = "latin-1" },
		"byte count":      func(t *HostTextFile) { t.ReturnedBytes = 3 },
		"line count":      func(t *HostTextFile) { t.ReturnedLines = 2 },
		"negative size":   func(t *HostTextFile) { t.Size = -1 },
		"reasonless":      func(t *HostTextFile) { t.Truncated = true },
		"unknown reason":  func(t *HostTextFile) { t.Truncated, t.TruncationReason = true, "other" },
		"nul content":     func(t *HostTextFile) { t.Content, t.ReturnedBytes = "h\x00", 2 },
		"bad modified at": func(t *HostTextFile) { t.ModifiedAt = "yesterday" },
	} {
		read := valid
		mutate(&read)
		if (HostFileRead{Payload: read}).Validate() == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	image := HostImageFile{SessionID: "s", Path: "/a.png", Size: 1, MediaType: "image/png", Width: 1, Height: 1, Data: []byte{1}}
	for name, mutate := range map[string]func(*HostImageFile){
		"tiff":        func(i *HostImageFile) { i.MediaType = "image/tiff" },
		"size":        func(i *HostImageFile) { i.Size = 2 },
		"no data":     func(i *HostImageFile) { i.Data, i.Size = nil, 0 },
		"zero width":  func(i *HostImageFile) { i.Width = 0 },
		"many pixels": func(i *HostImageFile) { i.Width, i.Height = 6000, 5000 },
	} {
		read := image
		mutate(&read)
		if (HostFileRead{Payload: read}).Validate() == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if (HostFileRead{}).Validate() == nil {
		t.Error("a read without a kind was accepted")
	}
}

func TestReadHostFileInputAcceptsAbsoluteAndHomePaths(t *testing.T) {
	for _, path := range []string{"/etc/hosts", "/", "~", "~/notes.md", "/tmp/with space.txt"} {
		if err := (ReadHostFileInput{Path: path}).Validate(); err != nil {
			t.Errorf("%q: %v", path, err)
		}
	}
	for _, path := range []string{"", "notes.md", "./a", "~other/a", "/a\nb", "/a\x00b", "/" + strings.Repeat("a", MaxWorkspacePathBytes)} {
		if (ReadHostFileInput{Path: path}).Validate() == nil {
			t.Errorf("%q was accepted", path)
		}
	}
}
