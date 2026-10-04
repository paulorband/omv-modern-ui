package httpx

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// SSEWriter writes Server-Sent Events.
type SSEWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

// NewSSE prepares the response for an event stream. It fails if streaming is unsupported.
func NewSSE(w http.ResponseWriter) (*SSEWriter, error) {
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, &Error{Status: http.StatusInternalServerError, Code: "internal", Message: "streaming não suportado"}
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // keep nginx from buffering
	w.WriteHeader(http.StatusOK)
	f.Flush()
	return &SSEWriter{w: w, f: f}, nil
}

// Send emits an event whose data is v encoded as JSON. An empty event name sends a default "message".
func (s *SSEWriter) Send(event string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if event != "" {
		if _, err := fmt.Fprintf(s.w, "event: %s\n", event); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", b); err != nil {
		return err
	}
	s.f.Flush()
	return nil
}

// Comment sends a keep-alive comment line.
func (s *SSEWriter) Comment(text string) error {
	if _, err := fmt.Fprintf(s.w, ": %s\n\n", text); err != nil {
		return err
	}
	s.f.Flush()
	return nil
}
