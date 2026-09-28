package internal

import (
	"context"
	"fmt"

	api "github.com/George1487/MalyginGeorge/internal/generated"
	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TripHistoryRepository struct {
	pool *pgxpool.Pool
}

func NewTripHistoryRepository(pool *pgxpool.Pool) *TripHistoryRepository {
	return &TripHistoryRepository{pool: pool}
}

func (repo *TripHistoryRepository) AddTransition(
	ctx context.Context,
	tripID api.TripId,
	fromStatus *api.TripStatus,
	toStatus api.TripStatus,
	reason *string) error {
	query, args, err := sq.
		Insert("trip_status_history").
		Columns(
			"trip_id",
			"from_status",
			"to_status",
			"reason",
		).
		Values(
			tripID,
			fromStatus,
			toStatus,
			reason,
		).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert trip status history query: %w", err)
	}

	tx, ok := ctx.Value(txKey{}).(pgx.Tx)
	if ok {
		_, err = tx.Exec(ctx, query, args...)
	} else {
		_, err = repo.pool.Exec(ctx, query, args...)
	}
	if err != nil {
		return fmt.Errorf("insert trip status history: %w", err)
	}

	return nil
}
