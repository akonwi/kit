package contract

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
	// The largest image show_image stores, as a Paper export at 2x can be.
	shown := valid
	shown.Size, shown.Width, shown.Height = MaxStoredImageBytes, 4740, 5063
	if err := shown.Validate(); err != nil {
		t.Fatalf("shown image: %v", err)
	}
	cases := []AttachmentInfo{valid, valid, valid, valid, valid, valid, valid, valid}
	cases[0].Filename = "../photo.png"
	cases[1].MediaType = "application/octet-stream"
	cases[2].SHA256 = "not-a-checksum"
	cases[3].Width = 0
	cases[4].Size = MaxStoredImageBytes + 1
	cases[5].Width, cases[5].Height = 4740, 5064
	cases[6].Width, cases[6].Height = MaxStoredImageSide+1, 2
	cases[7].MediaType, cases[7].Width, cases[7].Height, cases[7].Size = "text/plain", 0, 0, MaxTextAttachmentBytes+1
	for index, candidate := range cases {
		if err := candidate.Validate(); err == nil {
			t.Fatalf("case %d accepted", index)
		}
	}
}
