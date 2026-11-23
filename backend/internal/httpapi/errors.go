package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/RahulKumar1119/rukpay/backend/internal/guardian"
	"github.com/RahulKumar1119/rukpay/backend/internal/risk"
	"github.com/RahulKumar1119/rukpay/backend/internal/store"
	"github.com/RahulKumar1119/rukpay/backend/internal/vault"
)

// Error codes in the body of every non-2xx response.
const (
	codeInvalidField      = "invalid_field"
	codeInvalidIdentifier = "invalid_identifier"
	codeInvalidRequest    = "invalid_request"
	codeForbidden         = "forbidden"
	codeNotFound          = "not_found"
	codeMethodNotAllowed  = "method_not_allowed"
	codeStateConflict     = "state_conflict"
	codeTooLarge          = "too_large"
	codeInternal          = "internal"
)

// Calm messages for the error bodies. Each is fixed text: none is built from the
// request, the store or an error, so no submitted value, store text or stack
// trace can reach a response (Requirement 11.6).
const (
	msgInvalidAmount     = "Please check the amount and try again."
	msgInvalidAge        = "Please check how long ago the recipient was added and try again."
	msgInvalidRetries    = "Please check the number of retries and try again."
	msgInvalidField      = "Please check the payment details and try again."
	msgInvalidIdentifier = "One of the account details is not in the protected format. Please send masked details only."
	msgInvalidRequest    = "We could not read that request. Please check it and try again."
	msgForbidden         = "This approval link is not valid for this payment, or it has run out."
	msgNotFound          = "We could not find that payment."
	msgMethodNotAllowed  = "That action is not available here."
	msgStateConflict     = "This payment has already moved on. Here is where it stands now."
	msgTooLarge          = "That request is too large. Please try again with less information."
	msgInternal          = "Something went wrong on our side. Please try again in a moment."
	msgMissingVaultID    = "Please say which payment you mean."
)

// errorDetail is the error object in every error body.
type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

// errorBody is the one shape of every non-2xx response. A 409 also carries the
// current View, so a client that lost a race can update its screen without
// another request (Requirement 2.6).
type errorBody struct {
	Error errorDetail `json:"error"`
	Vault *vault.View `json:"vault,omitempty"`
}

// errBadJSON stands for any body that is not valid JSON of the expected shape.
var errBadJSON = errors.New("httpapi: invalid JSON body")

// classify maps an error from the services to a status and an error body. Any
// error it does not know becomes a 500 with a fixed message.
func classify(err error) (int, errorBody) {
	var (
		conflict *vault.ConflictError
		invalid  *risk.ValidationError
		tooBig   *http.MaxBytesError
	)
	switch {
	case errors.As(err, &conflict):
		view := conflict.Current
		return http.StatusConflict, errorBody{
			Error: errorDetail{Code: codeStateConflict, Message: msgStateConflict},
			Vault: &view,
		}
	case errors.As(err, &invalid):
		return http.StatusBadRequest, errorBody{Error: errorDetail{
			Code:    codeInvalidField,
			Message: invalidFieldMessage(invalid.Field),
			Field:   invalid.Field,
		}}
	case errors.Is(err, store.ErrUnmaskedIdentifier):
		return http.StatusBadRequest, errorBody{Error: errorDetail{Code: codeInvalidIdentifier, Message: msgInvalidIdentifier}}
	case errors.Is(err, errBadJSON), errors.Is(err, vault.ErrInvalidAnswers), errors.Is(err, guardian.ErrInvalidAction):
		return http.StatusBadRequest, errorBody{Error: errorDetail{Code: codeInvalidRequest, Message: msgInvalidRequest}}
	case errors.Is(err, guardian.ErrForbidden):
		return http.StatusForbidden, errorBody{Error: errorDetail{Code: codeForbidden, Message: msgForbidden}}
	case errors.Is(err, store.ErrNotFound), errors.Is(err, vault.ErrNotDemoMode):
		return http.StatusNotFound, errorBody{Error: errorDetail{Code: codeNotFound, Message: msgNotFound}}
	case errors.As(err, &tooBig):
		return http.StatusRequestEntityTooLarge, errorBody{Error: errorDetail{Code: codeTooLarge, Message: msgTooLarge}}
	default:
		return http.StatusInternalServerError, errorBody{Error: errorDetail{Code: codeInternal, Message: msgInternal}}
	}
}

// invalidFieldMessage returns the calm message for an invalid signal field.
func invalidFieldMessage(field string) string {
	switch field {
	case "amount_inr":
		return msgInvalidAmount
	case "beneficiary_age_hours":
		return msgInvalidAge
	case "rapid_retry_count":
		return msgInvalidRetries
	default:
		return msgInvalidField
	}
}

// writeJSON writes v as the response body with the given status. API responses
// can carry a guardian token, so they are never cached.
func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		status = http.StatusInternalServerError
		body = []byte(`{"error":{"code":"` + codeInternal + `","message":"` + msgInternal + `"}}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// writeError classifies err and writes the matching error response.
func writeError(w http.ResponseWriter, err error) {
	status, body := classify(err)
	writeJSON(w, status, body)
}

// writeCode writes an error response for a condition that has no error value.
func writeCode(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: message}})
}
