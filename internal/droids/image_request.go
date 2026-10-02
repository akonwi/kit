package droids

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/akonwi/kit/internal/droids/internal/imageprep"
)

// image_request.go — applies a model's ImagePolicy to request messages
// (ADR 0037). Preparation is request-scoped: it returns new messages and never
// mutates canonical history.

const (
	// imageRetentionWindow is the most images one request sends.
	imageRetentionWindow = 20
	// requestImageCacheBytes bounds the shared cache of prepared images.
	requestImageCacheBytes = 64 << 20
)

// requestImagePreparer is shared by every droid so prepared history images are
// reused across sessions and requests.
var requestImagePreparer = imageprep.New(requestImageCacheBytes)

// imageOutcome classifies what preparation did to one image.
type imageOutcome string

const (
	imageUnchanged imageOutcome = "unchanged"
	imagePrepared  imageOutcome = "prepared"
	imageOmitted   imageOutcome = "omitted"
)

// imageAdjustment records preparation of one image that was not sent
// unchanged.
type imageAdjustment struct {
	// Message is the index of the message containing the image.
	Message   int
	Placement ImagePlacement
	Outcome   imageOutcome
	// Reason explains an omission.
	Reason string
	// Source and prepared dimensions of a prepared image.
	SourceWidth, SourceHeight int
	Width, Height             int
}

// requestImage is one image located in request messages.
type requestImage struct {
	message   int
	content   int
	placement ImagePlacement
	filename  string
	mediaType string
	url       string

	outcome imageOutcome
	reason  string
	result  imageprep.Result
}

// prepareRequestImages applies policy to every image in messages. Images that
// conform are sent unchanged, others are fitted or re-encoded, and images that
// cannot be sent are replaced by a text placeholder stating why. A resize
// notice follows each downscaled image.
func prepareRequestImages(policy ImagePolicy, messages []Message) ([]Message, []imageAdjustment) {
	images := locateRequestImages(messages)
	if len(images) == 0 {
		return messages, nil
	}

	var eligible []*requestImage
	for index := range images {
		image := &images[index]
		if reason := imageIneligibility(policy, image); reason != "" {
			image.outcome, image.reason = imageOmitted, reason
			continue
		}
		eligible = append(eligible, image)
	}
	eligible = retainRecentImages(policy, eligible)

	fit := policy.Fit
	if rule := policy.ManyImages; rule != nil && len(eligible) > rule.Above {
		fit = composeImageFits(policy.Fit, rule.Fit)
	}
	target := imageprep.Target{Formats: policy.Formats, Fit: fit, MaxEncodedBytes: policy.MaxEncodedBytes}

	var sent []*requestImage
	for _, image := range eligible {
		if !strings.HasPrefix(strings.ToLower(image.url), "data:") {
			// Remote images the policy accepts pass through; Droids never
			// fetches them.
			image.outcome = imageUnchanged
			sent = append(sent, image)
			continue
		}
		data, err := decodeImageDataURL(image.url)
		if err != nil {
			image.outcome, image.reason = imageOmitted, "the image data is invalid"
			continue
		}
		result, err := requestImagePreparer.Prepare(data, target)
		if err != nil {
			image.outcome, image.reason = imageOmitted, imagePreparationReason(err)
			continue
		}
		image.result = result
		image.outcome = imageUnchanged
		if result.Changed {
			image.outcome = imagePrepared
		}
		sent = append(sent, image)
	}
	omitForRequestImageBytes(policy, sent)

	return rewriteRequestImages(messages, images)
}

func locateRequestImages(messages []Message) []requestImage {
	var images []requestImage
	for messageIndex, message := range messages {
		switch value := message.(type) {
		case UserMessage:
			images = appendInputImages(images, messageIndex, ImagePlacementUser, value.Content)
		case ContextMessage:
			images = appendInputImages(images, messageIndex, ImagePlacementContext, value.Content)
		case ToolResultMessage:
			for contentIndex, block := range value.Content {
				if file, ok := block.(FileContent); ok && isImageMediaType(file.MediaType) {
					images = append(images, requestImage{
						message: messageIndex, content: contentIndex, placement: ImagePlacementToolResult,
						filename: file.Filename, mediaType: file.MediaType, url: file.URL,
					})
				}
			}
		}
	}
	return images
}

func appendInputImages(images []requestImage, messageIndex int, placement ImagePlacement, content []InputContent) []requestImage {
	for contentIndex, block := range content {
		if file, ok := block.(FileInput); ok && isImageMediaType(file.MediaType) {
			images = append(images, requestImage{
				message: messageIndex, content: contentIndex, placement: placement,
				filename: file.Filename, mediaType: file.MediaType, url: file.URL,
			})
		}
	}
	return images
}

// imageIneligibility returns why policy cannot receive image at all, or "".
func imageIneligibility(policy ImagePolicy, image *requestImage) string {
	if !policy.AcceptsImages() {
		return "this model does not accept image input"
	}
	if !policy.Accepts(image.placement) {
		switch image.placement {
		case ImagePlacementToolResult:
			return "this model does not accept images in tool results"
		case ImagePlacementContext:
			return "this model does not accept images in context messages"
		default:
			return "this model does not accept images in user messages"
		}
	}
	scheme, _, _ := strings.Cut(image.url, ":")
	switch strings.ToLower(scheme) {
	case "data":
		return ""
	case "https":
		if policy.AcceptsSource(ImageSourceHTTPS) {
			return ""
		}
		return "this model does not accept remote image URLs"
	default:
		return "the image source is not supported"
	}
}

// retainRecentImages keeps the most recent images within the retention
// window. Older images are omitted in batches of half the window, so the set
// of omitted images, and with it the request prefix, changes once per batch
// rather than with every new image.
func retainRecentImages(policy ImagePolicy, images []*requestImage) []*requestImage {
	window := imageRetentionWindow
	if policy.MaxImages > 0 && policy.MaxImages < window {
		window = policy.MaxImages
	}
	if len(images) <= window {
		return images
	}
	batch := max(window/2, 1)
	omit := (len(images) - window + batch - 1) / batch * batch
	for _, image := range images[:omit] {
		image.outcome = imageOmitted
		image.reason = "only the most recent images in the conversation are sent"
	}
	return images[omit:]
}

// omitForRequestImageBytes omits the oldest sent images until the total
// encoded image size fits the policy.
func omitForRequestImageBytes(policy ImagePolicy, sent []*requestImage) {
	if policy.MaxRequestImageBytes <= 0 {
		return
	}
	var total int64
	for _, image := range sent {
		total += imageprep.EncodedLen(len(image.result.Data))
	}
	for _, image := range sent {
		if total <= policy.MaxRequestImageBytes {
			return
		}
		total -= imageprep.EncodedLen(len(image.result.Data))
		image.outcome = imageOmitted
		image.reason = "the request's total image size limit was reached"
		image.result = imageprep.Result{}
	}
}

func composeImageFits(first, second ImageFit) ImageFit {
	return func(w, h int) (int, int) {
		if first != nil {
			w, h = first(w, h)
		}
		return second(w, h)
	}
}

func imagePreparationReason(err error) string {
	switch {
	case errors.Is(err, imageprep.ErrUnsupportedFormat):
		return "the image format is not supported"
	case errors.Is(err, imageprep.ErrSourceTooLarge):
		return "the image is too large to process"
	case errors.Is(err, imageprep.ErrEncodedLimit):
		return "the image exceeds this model's size limit"
	default:
		return "the image could not be decoded"
	}
}

func decodeImageDataURL(rawURL string) ([]byte, error) {
	header, payload, ok := strings.Cut(rawURL, ",")
	if !ok || !strings.HasSuffix(strings.ToLower(header), ";base64") {
		return nil, fmt.Errorf("image data URL must be base64-encoded")
	}
	return base64.StdEncoding.DecodeString(payload)
}

func imageDataURL(mediaType string, data []byte) string {
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// rewriteRequestImages returns messages with each image's outcome applied and
// reports every image that was not sent unchanged.
func rewriteRequestImages(messages []Message, images []requestImage) ([]Message, []imageAdjustment) {
	byMessage := map[int][]*requestImage{}
	var adjustments []imageAdjustment
	for index := range images {
		image := &images[index]
		if image.outcome == imageUnchanged && !image.result.Changed {
			continue
		}
		byMessage[image.message] = append(byMessage[image.message], image)
		adjustment := imageAdjustment{Message: image.message, Placement: image.placement, Outcome: image.outcome, Reason: image.reason}
		if image.outcome == imagePrepared {
			adjustment.SourceWidth, adjustment.SourceHeight = image.result.SourceWidth, image.result.SourceHeight
			adjustment.Width, adjustment.Height = image.result.Width, image.result.Height
		}
		adjustments = append(adjustments, adjustment)
	}
	if len(byMessage) == 0 {
		return messages, nil
	}

	out := append([]Message(nil), messages...)
	for messageIndex, changed := range byMessage {
		position := imagePositions(images, messageIndex)
		switch value := out[messageIndex].(type) {
		case UserMessage:
			value.Content = rewriteInputContent(value.Content, changed, position)
			out[messageIndex] = value
		case ContextMessage:
			value.Content = rewriteInputContent(value.Content, changed, position)
			out[messageIndex] = value
		case ToolResultMessage:
			value.Content = rewriteResultContent(value.Content, changed, position)
			out[messageIndex] = value
		}
	}
	return out, adjustments
}

// imagePositions maps each image's content index in a message to its 1-based
// position and the message's image count.
func imagePositions(images []requestImage, messageIndex int) func(content int) (int, int) {
	var indexes []int
	for _, image := range images {
		if image.message == messageIndex {
			indexes = append(indexes, image.content)
		}
	}
	return func(content int) (int, int) {
		for position, index := range indexes {
			if index == content {
				return position + 1, len(indexes)
			}
		}
		return 0, len(indexes)
	}
}

func rewriteInputContent(content []InputContent, changed []*requestImage, position func(int) (int, int)) []InputContent {
	byContent := map[int]*requestImage{}
	for _, image := range changed {
		byContent[image.content] = image
	}
	out := make([]InputContent, 0, len(content)+len(changed))
	for index, block := range content {
		image, ok := byContent[index]
		if !ok {
			out = append(out, block)
			continue
		}
		if image.outcome == imageOmitted {
			out = append(out, TextInput{Text: omittedImageText(image)})
			continue
		}
		file := block.(FileInput)
		file.MediaType, file.URL = image.result.MediaType, imageDataURL(image.result.MediaType, image.result.Data)
		out = append(out, file)
		if notice := resizeNotice(image, position); notice != "" {
			out = append(out, TextInput{Text: notice})
		}
	}
	return out
}

func rewriteResultContent(content []ResultContent, changed []*requestImage, position func(int) (int, int)) []ResultContent {
	byContent := map[int]*requestImage{}
	for _, image := range changed {
		byContent[image.content] = image
	}
	out := make([]ResultContent, 0, len(content)+len(changed))
	for index, block := range content {
		image, ok := byContent[index]
		if !ok {
			out = append(out, block)
			continue
		}
		if image.outcome == imageOmitted {
			out = append(out, TextContent{Text: omittedImageText(image)})
			continue
		}
		file := block.(FileContent)
		file.MediaType, file.URL = image.result.MediaType, imageDataURL(image.result.MediaType, image.result.Data)
		out = append(out, file)
		if notice := resizeNotice(image, position); notice != "" {
			out = append(out, TextContent{Text: notice})
		}
	}
	return out
}

func omittedImageText(image *requestImage) string {
	if image.filename != "" {
		return fmt.Sprintf("[Image %q omitted: %s.]", image.filename, image.reason)
	}
	return fmt.Sprintf("[Image omitted: %s.]", image.reason)
}

func resizeNotice(image *requestImage, position func(int) (int, int)) string {
	if !image.result.Resized() {
		return ""
	}
	number, count := position(image.content)
	return fmt.Sprintf("[Image %d of %d was resized from %d×%d to %d×%d pixels.]", number, count,
		image.result.SourceWidth, image.result.SourceHeight, image.result.Width, image.result.Height)
}
