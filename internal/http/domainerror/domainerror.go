package domainerror

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/George1487/MalyginGeorge/internal/domain/trip"
	api "github.com/George1487/MalyginGeorge/internal/generated"
)

func Problem(err error) (int, api.Problem) {
	var validationErr *trip.ValidationError
	switch {
	case errors.As(err, &validationErr), errors.Is(err, trip.ErrInvalidRequest):
		detail := "Request validation failed"
		if validationErr != nil && validationErr.Message != "" {
			detail = validationErr.Error()
		}
		return http.StatusBadRequest, newProblem(
			http.StatusBadRequest,
			"invalid-request",
			"Invalid request",
			detail,
			"invalid_request",
		)
	case errors.Is(err, trip.ErrTripNotFound):
		return http.StatusNotFound, newProblem(
			http.StatusNotFound,
			"trip-not-found",
			"Trip not found",
			"Trip was not found",
			"trip_not_found",
		)
	case errors.Is(err, trip.ErrTripCompleted):
		return http.StatusConflict, newProblem(
			http.StatusConflict,
			"trip-completed",
			"Trip completed",
			"Operation is not allowed for a completed trip",
			"trip_completed",
		)
	case errors.Is(err, trip.ErrDriverBusy):
		return http.StatusConflict, newProblem(
			http.StatusConflict,
			"driver-busy",
			"Driver busy",
			"Driver already has an active trip",
			"driver_busy",
		)
	case errors.Is(err, trip.ErrIdempotencyConflict):
		return http.StatusConflict, newProblem(
			http.StatusConflict,
			"idempotency-conflict",
			"Idempotency conflict",
			"Idempotency-Key was already used with a different request body",
			"idempotency_conflict",
		)
	default:
		return http.StatusInternalServerError, newProblem(
			http.StatusInternalServerError,
			"internal-error",
			"Internal Server Error",
			"Internal server error",
			"internal_error",
		)
	}
}

func Write(w http.ResponseWriter, err error) {
	status, body := Problem(err)
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func newProblem(status int, problemType, title, detail, code string) api.Problem {
	return api.Problem{
		Type:   "https://tripgo.example/problems/" + problemType,
		Title:  title,
		Status: int32(status), // #nosec G115 -- HTTP status codes fit within int32
		Detail: &detail,
		Code:   code,
	}
}
