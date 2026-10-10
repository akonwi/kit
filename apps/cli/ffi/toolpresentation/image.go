package toolpresentation

import (
	"encoding/json"
	"strings"
)

// ShownImage is an image a show_image call presented to the user.
type ShownImage struct {
	// AttachmentID names the stored image; it is empty when the call shows
	// none.
	AttachmentID string
	Filename     string
	Caption      string
}

// ShowsImage returns the image a finished call presented, if it is a
// successful show_image call whose details mark one.
func ShowsImage(call Call, result Result) ShownImage {
	if !strings.EqualFold(call.Name, "show_image") || !result.Succeeded {
		return ShownImage{}
	}
	var details struct {
		Presentation string `json:"presentation"`
		AttachmentID string `json:"attachmentId"`
		Filename     string `json:"filename"`
		Caption      string `json:"caption"`
	}
	if json.Unmarshal([]byte(result.Details), &details) != nil ||
		details.Presentation != "transcript-image" || details.AttachmentID == "" {
		return ShownImage{}
	}
	return ShownImage{
		AttachmentID: details.AttachmentID,
		Filename:     details.Filename,
		Caption:      strings.TrimSpace(details.Caption),
	}
}
