package internal

import (
	"context"
	"fmt"
	"time"

	api "github.com/George1487/MalyginGeorge/internal/generated"
	"github.com/google/uuid"
)

type TripService struct {
	tripRepository *TripRepository

	tripHistoryRepository *TripHistoryRepository

	txManager TxManager
}

func NewTripService(tripRepository *TripRepository,
	tripHistoryRepository *TripHistoryRepository,
	txManager TxManager) *TripService {
	return &TripService{
		tripRepository:        tripRepository,
		tripHistoryRepository: tripHistoryRepository,
		txManager:             txManager,
	}
}

func (s *TripService) CreateTrip(ctx context.Context, tripData api.TripData) (*api.Trip, error) {

	trip := &api.Trip{
		Id:         uuid.New(),
		UserId:     tripData.UserId,
		DriverId:   tripData.DriverId,
		StartPoint: tripData.StartPoint,
		EndPoint:   tripData.EndPoint,
		Price:      tripData.Price,
		Status:     api.Active,
		StartedAt:  time.Now().UTC(),
		FinishedAt: nil,
	}

	err := s.txManager.Do(ctx, func(ctx context.Context) error {

		err := s.tripRepository.Save(ctx, trip)

		if err != nil {
			return fmt.Errorf("save trip: %w", err)
		}

		err = s.tripHistoryRepository.AddTransition(
			ctx,
			trip.Id,
			nil,
			api.Active,
			nil)
		if err != nil {
			return fmt.Errorf("save trip history: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("on transaction: %w", err)
	}
	return trip, nil
}

func (s *TripService) GetTrip(ctx context.Context, tripID api.TripId) (*api.Trip, error) {
	trip, err := s.tripRepository.GetTrip(ctx, tripID)
	if err != nil {
		return nil, fmt.Errorf("get trip: %w", err)
	}

	return trip, nil
}

func (s *TripService) FinishTrip(ctx context.Context, tripId uuid.UUID) (*api.Trip, error) {
	var trip *api.Trip

	err := s.txManager.Do(ctx, func(txCtx context.Context) error {
		var err error

		trip, err = s.tripRepository.GetTripForUpdate(txCtx, tripId)
		if err != nil {
			return fmt.Errorf("get trip for update: %w", err)
		}
		if trip.Status == api.Completed {
			return ErrTripCompleted
		}

		previousStatus := trip.Status
		finishedAt := time.Now().UTC()
		trip.Status = api.Completed
		trip.FinishedAt = &finishedAt

		err = s.tripRepository.Update(txCtx, trip)
		if err != nil {
			return fmt.Errorf("save trip: %w", err)
		}

		err = s.tripHistoryRepository.AddTransition(
			txCtx,
			trip.Id,
			&previousStatus,
			api.Completed,
			nil)
		if err != nil {
			return fmt.Errorf("save trip history: %w", err)
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("on transaction: %w", err)
	}

	return trip, nil
}
