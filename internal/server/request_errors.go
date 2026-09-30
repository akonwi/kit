package server

import (
	"log/slog"
	"net/http"
)

// requestErrorReporter records the cause of a failed request. Error responses
// deliberately omit internal causes, so this is the only place they surface.
type requestErrorReporter interface {
	reportRequestError(status int, err error)
}

// errorReportingWriter attaches request identity and the daemon logger to a
// response writer without changing its streaming capabilities.
type errorReportingWriter struct {
	http.ResponseWriter
	logger *slog.Logger
	method string
	path   string
}

func withRequestErrorReporting(writer http.ResponseWriter, request *http.Request, logger *slog.Logger) http.ResponseWriter {
	if logger == nil {
		return writer
	}
	return &errorReportingWriter{ResponseWriter: writer, logger: logger, method: request.Method, path: request.URL.Path}
}

func (w *errorReportingWriter) reportRequestError(status int, err error) {
	w.logger.Error("request failed", "method", w.method, "path", w.path, "status", status, "error", err)
}

// Flush preserves http.Flusher for server-sent event handlers.
func (w *errorReportingWriter) Flush() {
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

// Unwrap exposes the underlying writer to http.ResponseController.
func (w *errorReportingWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func reportRequestError(writer http.ResponseWriter, status int, err error) {
	if reporter, ok := writer.(requestErrorReporter); ok && err != nil {
		reporter.reportRequestError(status, err)
	}
}
