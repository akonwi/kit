package toolpresentation

import "testing"

func TestShowsImage(t *testing.T) {
	t.Parallel()
	details := `{"presentation":"transcript-image","attachmentId":"attachment_1","filename":"home.png",` +
		`"mediaType":"image/png","width":800,"height":600,"caption":" Home "}`
	call := Call{Name: "show_image", Arguments: `{"path":"home.png"}`}
	if got := ShowsImage(call, Result{Succeeded: true, Details: details}); got != (ShownImage{AttachmentID: "attachment_1", Filename: "home.png", Caption: "Home"}) {
		t.Fatalf("ShowsImage() = %+v", got)
	}
	for name, test := range map[string]struct {
		call   Call
		result Result
	}{
		"a failed call":     {call, Result{Details: details}},
		"another tool":      {Call{Name: "inspect_image"}, Result{Succeeded: true, Details: details}},
		"unmarked details":  {call, Result{Succeeded: true, Details: `{"attachmentId":"attachment_1"}`}},
		"no details":        {call, Result{Succeeded: true}},
		"malformed details": {call, Result{Succeeded: true, Details: `{`}},
	} {
		if got := ShowsImage(test.call, test.result); got.AttachmentID != "" {
			t.Errorf("%s: ShowsImage() = %+v, want none", name, got)
		}
	}
}
