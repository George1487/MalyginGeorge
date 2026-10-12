package internal

import "errors"

var (
	ErrInvalidRequest      = errors.New("invalid request")
	ErrTripNotFound        = errors.New("trip not found")
	ErrTripCompleted       = errors.New("trip completed")
	ErrDriverBusy          = errors.New("driver busy")
	ErrIdempotencyConflict = errors.New("idempotency conflict")
)
