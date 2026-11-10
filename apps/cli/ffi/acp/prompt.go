package acp

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"net/url"
	"path"
	"slices"
	"strings"
	"unicode"

	protocol "github.com/akonwi/kit/api/contract"
)

// promptRequest is an ACP prompt converted for Kit: either a prompt input or
// a prompt command with its arguments.
type promptRequest struct {
	input   protocol.PromptInput
	command string
	args    string
}

// attachment is binary prompt content Kit uploads as a session attachment.
type attachment struct {
	filename string
	data     []byte
}

// convertPrompt converts ACP content blocks to Kit prompt text and the
// attachments to upload. Text, links, and references stay in place; the
// contents of embedded text resources follow the text. Without acceptsImages,
// an image is omitted and a note in its place tells the model so.
func convertPrompt(blocks []contentBlock, acceptsImages bool) (string, []attachment, error) {
	var text, contexts strings.Builder
	var attachments []attachment
	for index, block := range blocks {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
		case "resource_link":
			if block.URI == "" {
				return "", nil, invalidParams("prompt block %d: resource_link requires a uri", index)
			}
			text.WriteString(linkText(block.URI, block.Name))
		case "resource":
			if block.Resource == nil || block.Resource.URI == "" {
				return "", nil, invalidParams("prompt block %d: resource requires a uri", index)
			}
			resource := block.Resource
			switch {
			case resource.Text != nil:
				text.WriteString(linkText(resource.URI, ""))
				writeContext(&contexts, resource.URI, *resource.Text)
			case resource.Blob != nil:
				data, err := base64.StdEncoding.DecodeString(*resource.Blob)
				if err != nil {
					return "", nil, invalidParams("prompt block %d: resource blob is not base64", index)
				}
				text.WriteString(linkText(resource.URI, ""))
				attachments = append(attachments, attachment{filename: resourceFilename(resource.URI, resource.MimeType, index), data: data})
			default:
				return "", nil, invalidParams("prompt block %d: resource requires text or blob", index)
			}
		case "image":
			data, err := base64.StdEncoding.DecodeString(block.Data)
			if err != nil || len(data) == 0 {
				return "", nil, invalidParams("prompt block %d: image data is not base64", index)
			}
			filename := resourceFilename(block.URI, block.MimeType, index)
			if !acceptsImages {
				fmt.Fprintf(&text, "[Image %q omitted: this model does not accept image input.]", filename)
				continue
			}
			attachments = append(attachments, attachment{filename: filename, data: data})
		case "audio":
			return "", nil, invalidParams("prompt block %d: Kit does not accept audio", index)
		default:
			return "", nil, invalidParams("prompt block %d: unknown content type %q", index, block.Type)
		}
	}
	if contexts.Len() > 0 {
		if text.Len() > 0 {
			text.WriteString("\n\n")
		}
		text.WriteString(strings.TrimRight(contexts.String(), "\n"))
	}
	return text.String(), attachments, nil
}

// linkText is how a link reads in prompt text: a file's absolute path, or
// another resource's name and URI.
func linkText(uri, name string) string {
	if parsed, err := url.Parse(uri); err == nil && parsed.Scheme == "file" && parsed.Path != "" {
		return parsed.Path
	}
	if name != "" && name != uri {
		return fmt.Sprintf("[%s](%s)", name, uri)
	}
	return uri
}

func writeContext(contexts *strings.Builder, uri, content string) {
	fence := "```"
	for strings.Contains(content, fence) {
		fence += "`"
	}
	fmt.Fprintf(contexts, "Contents of %s:\n\n%s\n%s\n%s\n\n", linkText(uri, ""), fence, strings.TrimSuffix(content, "\n"), fence)
}

func resourceFilename(uri, mimeType string, index int) string {
	if parsed, err := url.Parse(uri); err == nil && parsed.Path != "" {
		if name := path.Base(parsed.Path); name != "." && name != "/" {
			return name
		}
	}
	extension := ""
	if extensions, err := mime.ExtensionsByType(mimeType); err == nil && len(extensions) > 0 {
		extension = extensions[0]
	}
	return fmt.Sprintf("attachment-%d%s", index+1, extension)
}

// promptCommand parses text as an invocation of one of commands.
func promptCommand(text string, commands []protocol.PromptCommand) (string, string, bool) {
	trimmed := strings.TrimLeft(text, " \t\n")
	invocation, ok := strings.CutPrefix(trimmed, "/")
	if !ok {
		return "", "", false
	}
	name, args := invocation, ""
	if end := strings.IndexFunc(invocation, unicode.IsSpace); end >= 0 {
		name, args = invocation[:end], strings.TrimSpace(invocation[end:])
	}
	if name == compactCommand {
		return name, args, true
	}
	for _, command := range commands {
		if command.Name == name {
			return name, args, true
		}
	}
	return "", "", false
}

// compactCommand is the built-in command that compacts the session.
const compactCommand = "compact"

// preparePrompt converts blocks and uploads their attachments to handle.
func preparePrompt(ctx context.Context, handle SessionHandle, blocks []contentBlock, commands []protocol.PromptCommand, inputs []protocol.ModelInputKind) (promptRequest, error) {
	// A session whose model the server cannot resolve lists no inputs; its
	// images are left to the server's check.
	acceptsImages := len(inputs) == 0 || slices.Contains(inputs, protocol.ModelInputImage)
	text, attachments, err := convertPrompt(blocks, acceptsImages)
	if err != nil {
		return promptRequest{}, err
	}
	if len(attachments) == 0 {
		if name, args, ok := promptCommand(text, commands); ok {
			return promptRequest{command: name, args: args}, nil
		}
	}
	if strings.TrimSpace(text) == "" && len(attachments) == 0 {
		return promptRequest{}, invalidParams("prompt is empty")
	}
	request := promptRequest{input: protocol.PromptInput{Text: text}}
	for _, attached := range attachments {
		info, err := handle.UploadAttachment(ctx, attached.filename, bytes.NewReader(attached.data))
		if err != nil {
			return promptRequest{}, projectBackendError(err)
		}
		request.input.AttachmentIDs = append(request.input.AttachmentIDs, info.ID)
	}
	return request, nil
}
