package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/akonwi/kit/internal/protocol"
	"github.com/akonwi/kit/internal/session"
	"github.com/akonwi/kit/internal/version"
)

type pluginToastSource interface {
	Next(context.Context) (protocol.PluginToast, error)
	Close()
}

type runtimePluginToastSource struct {
	updates     <-chan session.PluginToast
	unsubscribe func()
}

func (s runtimePluginToastSource) Close() { s.unsubscribe() }
func (s runtimePluginToastSource) Next(ctx context.Context) (protocol.PluginToast, error) {
	select {
	case <-ctx.Done():
		return protocol.PluginToast{}, ctx.Err()
	case toast, ok := <-s.updates:
		if !ok {
			return protocol.PluginToast{}, io.EOF
		}
		projected := protocol.PluginToast{PluginID: toast.PluginID, Instance: toast.Instance, Title: toast.Title, Subtitle: toast.Subtitle, Variant: toast.Variant, Persistent: toast.Persistent}
		return projected, projected.Validate()
	}
}
func (s runtimeSessionService) SubscribePluginToasts(ctx context.Context, id string) (pluginToastSource, error) {
	updates, unsubscribe, err := s.manager.SubscribePluginToasts(ctx, id)
	if err != nil {
		return nil, err
	}
	return runtimePluginToastSource{updates, unsubscribe}, nil
}

func servePluginToasts(writer http.ResponseWriter, request *http.Request, service sessionService) {
	source, err := service.SubscribePluginToasts(request.Context(), request.PathValue("sessionID"))
	if err != nil {
		writeSessionError(writer, err)
		return
	}
	defer source.Close()
	writer.Header().Set("Content-Type", "application/x-ndjson")
	writer.Header().Set("Cache-Control", "no-store")
	controller := http.NewResponseController(writer)
	defer controller.SetWriteDeadline(time.Time{})
	// Subscription exists before response headers make this connection ready.
	_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := controller.Flush(); err != nil {
		return
	}
	for request.Context().Err() == nil {
		ctx, cancel := context.WithTimeout(request.Context(), 15*time.Second)
		toast, err := source.Next(ctx)
		cancel()
		_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if errors.Is(err, context.DeadlineExceeded) && request.Context().Err() == nil {
			if _, err := io.WriteString(writer, "\n"); err != nil {
				return
			}
		} else if err != nil {
			return
		} else {
			if err := toast.Validate(); err != nil {
				return
			}
			if err := json.NewEncoder(writer).Encode(toast); err != nil {
				return
			}
		}
		if err := controller.Flush(); err != nil {
			return
		}
	}
}

// StreamPluginToasts opens a fresh live-only stream, with no cursor or replay.
func (c *Client) StreamPluginToasts(ctx context.Context, id string) (io.ReadCloser, error) {
	registry, err := LoadRegistry(c.paths)
	if err != nil {
		return nil, err
	}
	if err := compatible(registry); err != nil {
		return nil, err
	}
	token, err := loadToken(c.paths)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, registry.URL+"/v1/sessions/"+url.PathEscape(id)+"/plugin-toasts", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set(instanceHeader, registry.InstanceID)
	request.Header.Set(protocolHeader, strconv.Itoa(version.SessionProtocolVersion))
	response, err := c.sessionHTTP.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		defer response.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(response.Body, maxSessionResponseBytes))
		return nil, decodeAPIError(response.StatusCode, body)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-ndjson" {
		response.Body.Close()
		return nil, fmt.Errorf("invalid plugin toast stream content type")
	}
	return response.Body, nil
}

// ReadPluginToasts validates bounded live frames and invokes receive in wire order.
// Closing the response body or cancelling its request terminates blocked reads.
func ReadPluginToasts(body io.Reader, receive func(protocol.PluginToast) error) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), 32*1024)
	for scanner.Scan() {
		if !utf8.Valid(scanner.Bytes()) {
			return errors.New("invalid UTF-8 in plugin toast frame")
		}
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var toast protocol.PluginToast
		if err := decodeStrictJSONObject(scanner.Bytes(), &toast); err != nil {
			return err
		}
		if err := toast.Validate(); err != nil {
			return err
		}
		if err := receive(toast); err != nil {
			return err
		}
	}
	return scanner.Err()
}
