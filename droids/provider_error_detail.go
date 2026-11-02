package droids

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxProviderDetailBytes bounds the provider's explanation retained on a
// failed response. It is long enough for an actionable sentence and short
// enough that a provider echoing request content cannot store much of it.
const maxProviderDetailBytes = 300

// maxProviderErrorBodyBytes bounds how much of an HTTP error body is read.
const maxProviderErrorBodyBytes = 64 << 10

var providerCode = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,63}$`)

// sanitizeProviderDetail makes a provider's error text safe to store and
// render. The result is valid single-line UTF-8 without control, format, or
// bidirectional characters, with whitespace collapsed, bounded to
// maxProviderDetailBytes. It does not redact content: what a provider says
// is kept as it said it, so only explanation fields are ever passed here.
// Applying it twice yields the same text.
func sanitizeProviderDetail(text string) string {
	text = strings.Map(func(character rune) rune {
		switch {
		case character == utf8.RuneError:
			return -1
		case unicode.IsControl(character), unicode.Is(unicode.Cf, character),
			unicode.In(character, unicode.Zl, unicode.Zp):
			return ' '
		default:
			return character
		}
	}, strings.ToValidUTF8(text, ""))
	return truncateProviderDetail(strings.Join(strings.Fields(text), " "))
}

func truncateProviderDetail(text string) string {
	text = strings.TrimSpace(text)
	if len(text) <= maxProviderDetailBytes {
		return text
	}
	const ellipsis = "…"
	end := maxProviderDetailBytes - len(ellipsis)
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return strings.TrimSpace(text[:end]) + ellipsis
}

// sanitizeProviderCode keeps a provider error code or type only when it is a
// short identifier, such as "model_not_found" or "invalid_request_error".
func sanitizeProviderCode(code string) string {
	code = strings.TrimSpace(code)
	if !providerCode.MatchString(code) {
		return ""
	}
	return code
}

// validProviderStatus keeps an HTTP status only when it reports a failure.
func validProviderStatus(status int) int {
	if status < 400 || status > 599 {
		return 0
	}
	return status
}

// providerErrorFromBody extracts a provider's error code and explanation from
// an HTTP error body. It reads the explanation fields of common JSON error
// envelopes rather than the whole document, so validation listings that echo
// request input are not retained. Plain text is the explanation itself; HTML
// pages, malformed JSON, and unrecognized JSON yield no explanation.
func providerErrorFromBody(body []byte) (code, detail string) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 || body[0] == '<' {
		return "", ""
	}
	if body[0] != '{' && body[0] != '[' && body[0] != '"' {
		return "", string(body)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var document any
	if decoder.Decode(&document) != nil {
		// Malformed or truncated JSON may be any part of the document,
		// including echoed request content.
		return "", ""
	}
	switch value := document.(type) {
	case string:
		return "", value
	case map[string]any:
		return providerErrorFromObject(value, 0)
	default:
		return "", ""
	}
}

func providerErrorFromObject(object map[string]any, depth int) (code, detail string) {
	if nested, ok := object["error"]; ok && depth < 2 {
		switch value := nested.(type) {
		case string:
			return jsonScalarText(object["code"]), value
		case map[string]any:
			code, detail = providerErrorFromObject(value, depth+1)
			if code == "" {
				code = jsonScalarText(object["code"])
			}
			if detail != "" || code != "" {
				return code, detail
			}
		}
	}
	for _, key := range []string{"message", "detail", "error_description", "error_message"} {
		if text, ok := object[key].(string); ok && strings.TrimSpace(text) != "" {
			detail = text
			break
		}
	}
	code = jsonScalarText(object["code"])
	if kind := jsonScalarText(object["type"]); code == "" && kind != "error" {
		code = kind
	}
	return code, detail
}

func jsonScalarText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}

// providerHTTPErrorBody reads a failed HTTP response's buffered body. SDKs
// rebuffer error bodies after reading them; a successful streaming response
// is never read here, since a mid-stream error must not block on it.
func providerHTTPErrorBody(response *http.Response) []byte {
	if response == nil || response.Body == nil || validProviderStatus(response.StatusCode) == 0 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxProviderErrorBodyBytes))
	_ = response.Body.Close()
	response.Body = io.NopCloser(bytes.NewReader(body))
	return body
}

// newProviderError returns the typed error for a failure of kind, with the
// default retry classification.
func newProviderError(kind ProviderErrorKind) *ProviderError {
	return &ProviderError{
		Kind:      kind,
		Retryable: kind == ProviderRateLimit || kind == ProviderTransport || kind == ProviderInternal || kind == ProviderProtocol,
	}
}

// failedProviderKind is the error kind a terminal provider message reports,
// inferring one from its stop reason when the adapter set none.
func failedProviderKind(message AssistantMessage) ProviderErrorKind {
	kind := message.ErrorKind
	if kind == "" && message.StopReason == StopReasonContextWindow {
		kind = ProviderContextWindow
	}
	if kind == "" && message.StopReason == StopReasonError {
		kind = ProviderInternal
	}
	return kind
}

// attachProviderResponse records what a provider said about its failure:
// the HTTP status, its error code, and its explanation. Adapters call it only
// with values taken from a provider error response, never with transport or
// local validation text. Aborted messages and policy stops are unchanged.
// The values are sanitized here, so a provider stream never publishes raw
// provider text; the runtime sanitizes them again for providers that build
// their own errors.
func attachProviderResponse(message *AssistantMessage, status int, code, detail string) {
	if message.StopReason == StopReasonAborted || message.IsPolicyStop() {
		return
	}
	kind := failedProviderKind(*message)
	if kind == "" {
		return
	}
	if message.Error == nil {
		message.Error = newProviderError(kind)
	}
	message.Error.Status = status
	if message.Error.Code == "" {
		message.Error.Code = code
	}
	message.Error.Detail = detail
	sanitizeProviderResponse(message.Error)
}
