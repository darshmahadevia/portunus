package wire

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"strings"
)

const MaxRequestBytes = 64 << 10

func WriteError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorResponse{Error: APIError{Code: code, Message: message}})
}

func ReadChatRequest(w http.ResponseWriter, r *http.Request) (ChatRequest, []byte, bool) {
	var request ChatRequest
	defer r.Body.Close()
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use POST.")
		return request, nil, false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		WriteError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use application/json.")
		return request, nil, false
	}
	limit := int64(MaxRequestBytes)
	if r.Context().Value(internalLimitKey{}) == true {
		limit = 512 << 10
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			WriteError(w, http.StatusRequestEntityTooLarge, "request_too_large", "JSON body exceeds 64 KiB.")
		} else {
			WriteError(w, http.StatusBadRequest, "invalid_request", "Could not read JSON body.")
		}
		return request, nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON request.")
		return request, nil, false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		WriteError(w, http.StatusBadRequest, "invalid_request", "Expected one JSON object.")
		return request, nil, false
	}
	if len(request.Messages) < 1 || len(request.Messages) > 32 {
		WriteError(w, http.StatusBadRequest, "invalid_request", "Provide 1 to 32 messages.")
		return request, nil, false
	}
	for _, message := range request.Messages {
		if message.Role != "system" && message.Role != "user" && message.Role != "assistant" {
			WriteError(w, http.StatusBadRequest, "invalid_request", "Message role must be system, user, or assistant.")
			return request, nil, false
		}
		if strings.TrimSpace(message.Content) == "" {
			WriteError(w, http.StatusBadRequest, "invalid_request", "Message content must not be blank.")
			return request, nil, false
		}
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(body, &fields)
	if raw, exists := fields["max_output_tokens"]; exists && (string(raw) == "null" || request.MaxOutputTokens == 0) {
		WriteError(w, 400, "invalid_request", "max_output_tokens must be 1..4096.")
		return request, nil, false
	}
	if request.Model == "" {
		request.Model = "general"
	}
	if request.MaxOutputTokens == 0 {
		request.MaxOutputTokens = 256
	}
	if request.MaxOutputTokens < 1 || request.MaxOutputTokens > 4096 || math.IsNaN(request.Temperature) || math.IsInf(request.Temperature, 0) || request.Temperature < 0 || request.Temperature > 2 || len(request.Model) > 128 {
		WriteError(w, 400, "invalid_request", "Invalid model or generation settings.")
		return request, nil, false
	}
	return request, body, true
}

func Healthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, "{\"status\":\"ok\"}\n")
}

type internalLimitKey struct{}

// InternalRequest permits normalized JSON expansion while retaining a bounded provider input.
func InternalRequest(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), internalLimitKey{}, true))
}
