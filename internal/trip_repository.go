package internal

import (
	"context"
	"errors"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	api "github.com/George1487/MalyginGeorge/internal/generated"
)

type TripRepository struct {
	pool *pgxpool.Pool
}

func NewTripRepository(pool *pgxpool.Pool) *TripRepository {
	return &TripRepository{pool: pool}
}

func (repo *TripRepository) GetTrip(ctx context.Context, id api.TripId) (*api.Trip, error) {
	query, args, err := sq.
		Select(
			"id",
			"user_id",
			"driver_id",
			"start_latitude",
			"start_longitude",
			"end_latitude",
			"end_longitude",
			"price",
			"status",
			"started_at",
			"finished_at",
		).
		From("trips").
		Where(sq.Eq{"id": id}).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select trip query: %w", err)
	}

	var trip api.Trip

	tx, ok := ctx.Value(txKey{}).(pgx.Tx)

	if ok {
		err = tx.QueryRow(ctx, query, args...).Scan(
			&trip.Id,
			&trip.UserId,
			&trip.DriverId,
			&trip.StartPoint.Latitude,
			&trip.StartPoint.Longitude,
			&trip.EndPoint.Latitude,
			&trip.EndPoint.Longitude,
			&trip.Price,
			&trip.Status,
			&trip.StartedAt,
			&trip.FinishedAt,
		)
	} else {
		err = repo.pool.QueryRow(ctx, query, args...).Scan(
			&trip.Id,
			&trip.UserId,
			&trip.DriverId,
			&trip.StartPoint.Latitude,
			&trip.StartPoint.Longitude,
			&trip.EndPoint.Latitude,
			&trip.EndPoint.Longitude,
			&trip.Price,
			&trip.Status,
			&trip.StartedAt,
			&trip.FinishedAt,
		)
	}

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTripNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get trip: %w", err)
	}

	return &trip, nil
}

func (repo *TripRepository) Save(ctx context.Context, trip *api.Trip) error {
	query, args, err := sq.Insert("trips").
		Columns("id",
			"user_id",
			"driver_id",
			"start_latitude",
			"start_longitude",
			"end_latitude",
			"end_longitude",
			"price",
			"status",
			"started_at",
			"finished_at").
		Values(trip.Id,
			trip.UserId,
			trip.DriverId,
			trip.StartPoint.Latitude,
			trip.StartPoint.Longitude,
			trip.EndPoint.Latitude,
			trip.EndPoint.Longitude,
			trip.Price,
			trip.Status,
			trip.StartedAt,
			trip.FinishedAt).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert trip: %w", err)
	}

	tx, ok := ctx.Value(txKey{}).(pgx.Tx)
	if ok {
		_, err = tx.Exec(ctx, query, args...)
	} else {
		_, err = repo.pool.Exec(ctx, query, args...)
	}
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) &&
		pgErr.Code == "23505" &&
		pgErr.ConstraintName == "trips_one_active_per_driver_idx" {
		return fmt.Errorf("save trip: %w", errors.Join(ErrDriverBusy, err))
	}

	return fmt.Errorf("save trip: %w", err)
}

func (repo *TripRepository) GetTripForUpdate(ctx context.Context, id api.TripId) (*api.Trip, error) {
	query, args, err := sq.
		Select(
			"id",
			"user_id",
			"driver_id",
			"start_latitude",
			"start_longitude",
			"end_latitude",
			"end_longitude",
			"price",
			"status",
			"started_at",
			"finished_at",
		).
		From("trips").
		Where(sq.Eq{"id": id}).
		Suffix("FOR UPDATE").
		PlaceholderFormat(sq.Dollar).
		ToSql()

	if err != nil {
		return nil, fmt.Errorf("build select trip query: %w", err)
	}

	var trip api.Trip

	tx := ctx.Value(txKey{}).(pgx.Tx)

	err = tx.QueryRow(ctx, query, args...).Scan(
		&trip.Id,
		&trip.UserId,
		&trip.DriverId,
		&trip.StartPoint.Latitude,
		&trip.StartPoint.Longitude,
		&trip.EndPoint.Latitude,
		&trip.EndPoint.Longitude,
		&trip.Price,
		&trip.Status,
		&trip.StartedAt,
		&trip.FinishedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTripNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get trip: %w", err)
	}

	return &trip, nil
}

func (repo *TripRepository) Update(ctx context.Context, trip *api.Trip) error {
	query, args, err := sq.
		Update("trips").
		Set("user_id", trip.UserId).
		Set("driver_id", trip.DriverId).
		Set("start_latitude", trip.StartPoint.Latitude).
		Set("start_longitude", trip.StartPoint.Longitude).
		Set("end_latitude", trip.EndPoint.Latitude).
		Set("end_longitude", trip.EndPoint.Longitude).
		Set("price", trip.Price).
		Set("status", trip.Status).
		Set("started_at", trip.StartedAt).
		Set("finished_at", trip.FinishedAt).
		Set("updated_at", sq.Expr("now()")).
		Where(sq.Eq{"id": trip.Id}).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return fmt.Errorf("build update trip query: %w", err)
	}

	var rowsAffected int64
	tx, ok := ctx.Value(txKey{}).(pgx.Tx)
	if ok {
		commandTag, execErr := tx.Exec(ctx, query, args...)
		err = execErr
		rowsAffected = commandTag.RowsAffected()
	} else {
		commandTag, execErr := repo.pool.Exec(ctx, query, args...)
		err = execErr
		rowsAffected = commandTag.RowsAffected()
	}
	if err != nil {
		return fmt.Errorf("update trip: %w", err)
	}
	if rowsAffected == 0 {
		return ErrTripNotFound
	}

	return nil
}
