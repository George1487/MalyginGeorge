package internal

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	api "github.com/George1487/MalyginGeorge/internal/generated"
)

type Server struct {
	tripService *TripService
	pool        *pgxpool.Pool
}

func NewServer(tripService *TripService, pool *pgxpool.Pool) *Server {
	return &Server{
		tripService: tripService,
		pool:        pool,
	}
}

func (s *Server) CreateTrip(
	w http.ResponseWriter,
	request *http.Request,
	_ api.CreateTripParams,
) {
	tripData, err := decodeCreateTripRequest(request.Body)
	if err != nil {
		writeError(w, fmt.Errorf("decode create trip request: %w", ErrInvalidRequest))
		return
	}

	trip, err := s.tripService.CreateTrip(request.Context(), tripData)
	if err != nil {
		writeError(w, err)
		return
	}

	w.Header().Set("Location", "/api/v1/trips/"+trip.Id.String())
	writeJSON(w, http.StatusCreated, trip)
}

func decodeCreateTripRequest(body io.Reader) (api.TripData, error) {
	var raw json.RawMessage
	decoder := json.NewDecoder(body)
	if err := decoder.Decode(&raw); err != nil {
		return api.TripData{}, fmt.Errorf("decode request body: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return api.TripData{}, errors.New("request body must contain one JSON value")
	}

	fields, err := requiredJSONObject(raw, "user_id", "driver_id", "start_point", "end_point", "price")
	if err != nil {
		return api.TripData{}, err
	}
	if _, err = requiredJSONObject(fields["start_point"], "latitude", "longitude"); err != nil {
		return api.TripData{}, fmt.Errorf("validate start_point: %w", err)
	}
	if _, err = requiredJSONObject(fields["end_point"], "latitude", "longitude"); err != nil {
		return api.TripData{}, fmt.Errorf("validate end_point: %w", err)
	}

	var tripData api.TripData
	strictDecoder := json.NewDecoder(bytes.NewReader(raw))
	strictDecoder.DisallowUnknownFields()
	if err = strictDecoder.Decode(&tripData); err != nil {
		return api.TripData{}, fmt.Errorf("decode trip data: %w", err)
	}

	if tripData.UserId == uuid.Nil || tripData.DriverId == uuid.Nil ||
		!coordinatesValid(tripData.StartPoint) ||
		!coordinatesValid(tripData.EndPoint) ||
		tripData.Price < 0 {
		return api.TripData{}, errors.New("trip data failed validation")
	}

	return tripData, nil
}

func requiredJSONObject(raw json.RawMessage, requiredFields ...string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, errors.New("JSON object is required")
	}

	for _, field := range requiredFields {
		value, ok := fields[field]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, fmt.Errorf("field %q is required", field)
		}
	}

	return fields, nil
}

func coordinatesValid(coordinates api.Coordinates) bool {
	return coordinates.Latitude >= -90 && coordinates.Latitude <= 90 &&
		coordinates.Longitude >= -180 && coordinates.Longitude <= 180
}

func (s *Server) GetTrip(
	w http.ResponseWriter,
	request *http.Request,
	tripID api.TripId,
) {
	trip, err := s.tripService.GetTrip(request.Context(), tripID)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, trip)
}

func (s *Server) FinishTrip(
	w http.ResponseWriter,
	request *http.Request,
	tripID api.TripId,
) {
	trip, err := s.tripService.FinishTrip(request.Context(), tripID)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, trip)
}

func (s *Server) Health(
	w http.ResponseWriter,
	request *http.Request,
) {
	writeJSON(w, http.StatusOK, api.HealthResponse{Status: api.Ok})
}

func (s *Server) Ready(
	w http.ResponseWriter,
	request *http.Request,
) {
	if s.pool == nil || s.pool.Ping(request.Context()) != nil {
		writeJSON(w, http.StatusServiceUnavailable, api.HealthResponse{Status: api.Unavailable})
		return
	}

	writeJSON(w, http.StatusOK, api.HealthResponse{Status: api.Ok})
}

// ErrorHandler converts errors raised by the generated request wrapper into the
// error format described by the OpenAPI contract.
func ErrorHandler(w http.ResponseWriter, _ *http.Request, _ error) {
	writeError(w, ErrInvalidRequest)
}

func writeError(w http.ResponseWriter, err error) {
	problem := problemFromError(err)

	w.Header().Set("Content-Type", "application/problem+json")
	writeJSON(w, int(problem.Status), problem)
}

func problemFromError(err error) api.Problem {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		return newProblem(
			http.StatusBadRequest,
			"invalid-request",
			"Invalid request",
			"Request validation failed",
			"invalid_request",
		)
	case errors.Is(err, ErrTripNotFound):
		return newProblem(
			http.StatusNotFound,
			"trip-not-found",
			"Trip not found",
			"Trip was not found",
			"trip_not_found",
		)
	case errors.Is(err, ErrTripCompleted):
		return newProblem(
			http.StatusConflict,
			"trip-completed",
			"Trip completed",
			"Operation is not allowed for a completed trip",
			"trip_completed",
		)
	case errors.Is(err, ErrDriverBusy):
		return newProblem(
			http.StatusConflict,
			"driver-busy",
			"Driver busy",
			"Driver already has an active trip",
			"driver_busy",
		)
	case errors.Is(err, ErrIdempotencyConflict):
		return newProblem(
			http.StatusConflict,
			"idempotency-conflict",
			"Idempotency conflict",
			"Idempotency-Key was already used with a different request body",
			"idempotency_conflict",
		)
	default:
		return newProblem(
			http.StatusInternalServerError,
			"internal-error",
			"Internal Server Error",
			"Internal server error",
			"internal_error",
		)
	}
}

func newProblem(status int, problemType, title, detail, code string) api.Problem {
	return api.Problem{
		Type:   "https://tripgo.example/problems/" + problemType,
		Title:  title,
		Status: int32(status),
		Detail: &detail,
		Code:   code,
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		return
	}
}

var _ api.ServerInterface = (*Server)(nil)
