package httpapi

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/config"
)

type fieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type errorPayload struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code      string       `json:"code"`
	Message   string       `json:"message"`
	Details   []fieldError `json:"details,omitempty"`
	RequestID string       `json:"request_id,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code, message string, details []fieldError) {
	writeJSON(w, status, errorPayload{
		Error: apiError{
			Code:    code,
			Message: message,
			Details: details,
		},
	})
}

func writeValidationError(w http.ResponseWriter, err config.ValidationError) {
	details := make([]fieldError, 0, len(err.Fields))
	for _, field := range err.Fields {
		details = append(details, fieldError{Field: field.Field, Message: field.Message})
	}
	writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "One or more fields are invalid.", details)
}

func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed.", nil)
}

func decodeJSON(r *http.Request, target interface{}) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(target); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return io.ErrUnexpectedEOF
	}
	return nil
}
