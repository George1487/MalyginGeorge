//go:build integration

package trip_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/George1487/MalyginGeorge/internal/domain/trip"
	"github.com/George1487/MalyginGeorge/internal/infra/postgres/transaction"
	triprepo "github.com/George1487/MalyginGeorge/internal/repository/trip"
)

func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		// #nosec G101 -- default local dev postgres connection string for test environment
		dbURL = "postgres://tripgo:tripgo@localhost:22032/tripgo?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)

	err = pool.Ping(ctx)
	require.NoError(t, err, "database must be reachable for integration tests")

	t.Cleanup(func() {
		pool.Close()
	})

	return pool
}

func TestRepository_Save_and_Get(t *testing.T) {
	pool := getTestPool(t)
	repo := triprepo.New(pool)
	ctx := context.Background()

	newTrip := trip.Trip{
		ID:       uuid.New(),
		UserID:   uuid.New(),
		DriverID: uuid.New(),
		StartPoint: trip.Coordinates{
			Latitude:  55.7558,
			Longitude: 37.6173,
		},
		EndPoint: trip.Coordinates{
			Latitude:  55.7522,
			Longitude: 37.6155,
		},
		Price:     50000,
		Status:    trip.StatusActive,
		StartedAt: time.Now().Truncate(time.Microsecond).UTC(),
	}

	err := repo.Save(ctx, newTrip)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM trips WHERE id = $1", newTrip.ID)
	})

	gotTrip, err := repo.GetTrip(ctx, newTrip.ID)
	require.NoError(t, err)
	require.Equal(t, newTrip.ID, gotTrip.ID)
	require.Equal(t, newTrip.UserID, gotTrip.UserID)
	require.Equal(t, newTrip.DriverID, gotTrip.DriverID)
	require.Equal(t, newTrip.StartPoint.Latitude, gotTrip.StartPoint.Latitude)
	require.Equal(t, newTrip.StartPoint.Longitude, gotTrip.StartPoint.Longitude)
	require.Equal(t, newTrip.EndPoint.Latitude, gotTrip.EndPoint.Latitude)
	require.Equal(t, newTrip.EndPoint.Longitude, gotTrip.EndPoint.Longitude)
	require.Equal(t, newTrip.Price, gotTrip.Price)
	require.Equal(t, newTrip.Status, gotTrip.Status)
}

func TestRepository_Update(t *testing.T) {
	pool := getTestPool(t)
	repo := triprepo.New(pool)
	ctx := context.Background()

	testTrip := trip.Trip{
		ID:       uuid.New(),
		UserID:   uuid.New(),
		DriverID: uuid.New(),
		StartPoint: trip.Coordinates{
			Latitude:  55.7558,
			Longitude: 37.6173,
		},
		EndPoint: trip.Coordinates{
			Latitude:  55.7522,
			Longitude: 37.6155,
		},
		Price:     35000,
		Status:    trip.StatusActive,
		StartedAt: time.Now().Truncate(time.Microsecond).UTC(),
	}

	err := repo.Save(ctx, testTrip)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM trips WHERE id = $1", testTrip.ID)
	})

	finishTime := time.Now().Truncate(time.Microsecond).UTC()
	testTrip.Status = trip.StatusCompleted
	testTrip.FinishedAt = &finishTime

	err = repo.Update(ctx, testTrip)
	require.NoError(t, err)

	gotTrip, err := repo.GetTrip(ctx, testTrip.ID)
	require.NoError(t, err)
	require.Equal(t, trip.StatusCompleted, gotTrip.Status)
	require.NotNil(t, gotTrip.FinishedAt)
}

func TestRepository_Transaction_Rollback_On_History_Error(t *testing.T) {
	pool := getTestPool(t)
	repo := triprepo.New(pool)
	txManager := transaction.NewPgxTxManager(pool)
	ctx := context.Background()

	tripID := uuid.New()
	newTrip := trip.Trip{
		ID:       tripID,
		UserID:   uuid.New(),
		DriverID: uuid.New(),
		StartPoint: trip.Coordinates{
			Latitude:  55.7558,
			Longitude: 37.6173,
		},
		EndPoint: trip.Coordinates{
			Latitude:  55.7522,
			Longitude: 37.6155,
		},
		Price:     40000,
		Status:    trip.StatusActive,
		StartedAt: time.Now().Truncate(time.Microsecond).UTC(),
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM trips WHERE id = $1", tripID)
	})

	txErr := txManager.Do(ctx, func(txCtx context.Context) error {
		// 1. Save trip inside transaction
		if err := repo.Save(txCtx, newTrip); err != nil {
			return err
		}

		// 2. Add transition with invalid to_status that fails CHECK constraint in PostgreSQL
		invalidStatus := trip.Status("non_existent_invalid_status")
		if err := repo.AddTransition(txCtx, tripID, nil, invalidStatus, nil); err != nil {
			return err
		}

		return nil
	})

	require.Error(t, txErr, "transaction should fail due to check constraint in trip_status_history")

	// 3. Verify that the trip was rolled back and is NOT present in trips table
	_, getErr := repo.GetTrip(ctx, tripID)
	require.Error(t, getErr)
	require.ErrorIs(t, getErr, trip.ErrTripNotFound)

	// 4. Verify that trip_status_history has no records for this trip
	var historyCount int
	err := pool.QueryRow(ctx, "SELECT count(*) FROM trip_status_history WHERE trip_id = $1", tripID).Scan(&historyCount)
	require.NoError(t, err)
	require.Equal(t, 0, historyCount)
}
