package protocol

import (
	"strings"
	"testing"

	"github.com/akonwi/kit/internal/identifier"
)

func TestSessionFileIndexValidate(t *testing.T) {
	t.Parallel()
	id, err := identifier.New("session_")
	if err != nil {
		t.Fatal(err)
	}
	valid := SessionFileIndex{SessionID: id, CWD: "/workspace", Entries: []FileIndexEntry{{Path: "internal/", IsDir: true}, {Path: "internal/app.go"}}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid index: %v", err)
	}
	cases := []SessionFileIndex{
		{SessionID: "bad", CWD: "/workspace", Entries: []FileIndexEntry{}},
		{SessionID: id, CWD: "", Entries: []FileIndexEntry{}},
		{SessionID: id, CWD: "relative", Entries: []FileIndexEntry{}},
		{SessionID: id, CWD: "/workspace", Entries: nil},
		{SessionID: id, CWD: "/workspace", Entries: []FileIndexEntry{{Path: "../secret"}}},
		{SessionID: id, CWD: "/workspace", Entries: []FileIndexEntry{{Path: "directory/", IsDir: false}}},
		{SessionID: id, CWD: "/workspace", Entries: []FileIndexEntry{{Path: "bad\nname"}}},
		{SessionID: id, CWD: "/workspace", Entries: []FileIndexEntry{{Path: strings.Repeat("a", MaxFileIndexPathLen+1)}}},
	}
	for index, candidate := range cases {
		if err := candidate.Validate(); err == nil {
			t.Errorf("case %d validated unexpectedly: %+v", index, candidate)
		}
	}
}
