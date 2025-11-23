package errors

import (
	"encoding/json"
	"net/http"
)

// ErrorResponse represents a standardized error response
type ErrorResponse struct {
	Error   string                 `json:"error"`
	Message string                 `json:"message"`
	Field   string                 `json:"field,omitempty"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// RespondError writes a standardized error response to the HTTP response writer
func RespondError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error:   code,
		Message: message,
	})
}

// RespondValidationError writes a validation error response with field information
func RespondValidationError(w http.ResponseWriter, code, message, field string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error:   code,
		Message: message,
		Field:   field,
	})
}

// RespondErrorWithDetails writes an error response with additional details
func RespondErrorWithDetails(w http.ResponseWriter, status int, code, message string, details map[string]interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error:   code,
		Message: message,
		Details: details,
	})
}

// RespondInternalError writes an internal server error response
func RespondInternalError(w http.ResponseWriter, message string) {
	RespondError(w, http.StatusInternalServerError, ErrCodeInternalError, message)
}

// RespondNotFound writes a not found error response
func RespondNotFound(w http.ResponseWriter, code, message string) {
	RespondError(w, http.StatusNotFound, code, message)
}

// RespondUnauthorized writes an unauthorized error response
func RespondUnauthorized(w http.ResponseWriter, code, message string) {
	RespondError(w, http.StatusUnauthorized, code, message)
}

// RespondForbidden writes a forbidden error response
func RespondForbidden(w http.ResponseWriter, code, message string) {
	RespondError(w, http.StatusForbidden, code, message)
}

// RespondBadRequest writes a bad request error response
func RespondBadRequest(w http.ResponseWriter, code, message string) {
	RespondError(w, http.StatusBadRequest, code, message)
}

// RespondServiceUnavailable writes a service unavailable error response
func RespondServiceUnavailable(w http.ResponseWriter, code, message string) {
	RespondError(w, http.StatusServiceUnavailable, code, message)
}

