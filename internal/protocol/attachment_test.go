package protocol

import "testing"

func TestAttachmentInfoValidate(t *testing.T) {
	t.Parallel()
	valid := AttachmentInfo{
		ID: "attachment_0123456789abcdef0123456789abcdef", SessionID: "session_one", Filename: "photo.png", MediaType: "image/png",
		Size: 12, SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		CreatedAt: "2026-01-02T03:04:05Z", Width: 2, Height: 3,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid attachment: %v", err)
	}
	cases := []AttachmentInfo{valid, valid, valid, valid, valid}
	cases[0].Filename = "../photo.png"
	cases[1].MediaType = "application/octet-stream"
	cases[2].SHA256 = "not-a-checksum"
	cases[3].Width = 0
	cases[4].Size = MaxImageAttachmentBytes + 1
	for index, candidate := range cases {
		if err := candidate.Validate(); err == nil {
			t.Fatalf("case %d accepted", index)
		}
	}
}
