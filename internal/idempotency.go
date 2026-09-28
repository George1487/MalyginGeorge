package internal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	api "github.com/George1487/MalyginGeorge/internal/generated"
)

const maxIdempotentRequestBody = 1 << 20

var errRollbackIdempotentRequest = errors.New("rollback idempotent request")

type idempotencyRecord struct {
	requestHash  []byte
	responseBody []byte
	contentType  string
	location     string
}

type databaseExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type IdempotencyStore struct {
	pool *pgxpool.Pool
}

func NewIdempotencyStore(pool *pgxpool.Pool) *IdempotencyStore {
	return &IdempotencyStore{pool: pool}
}

func (store *IdempotencyStore) LockAndGet(
	ctx context.Context,
	key uuid.UUID,
) (*idempotencyRecord, error) {
	executor := store.executor(ctx)

	lockQuery, lockArgs, err := sq.Select().
		Column("pg_advisory_xact_lock(?)", advisoryLockID(key)).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build idempotency lock query: %w", err)
	}
	if _, err = executor.Exec(ctx, lockQuery, lockArgs...); err != nil {
		return nil, fmt.Errorf("lock idempotency key: %w", err)
	}

	deleteQuery, deleteArgs, err := sq.Delete("idempotency_keys").
		Where(sq.Eq{"idempotency_key": key}).
		Where(sq.LtOrEq{"expires_at": time.Now().UTC()}).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build expired idempotency key query: %w", err)
	}
	if _, err = executor.Exec(ctx, deleteQuery, deleteArgs...); err != nil {
		return nil, fmt.Errorf("delete expired idempotency key: %w", err)
	}

	selectQuery, selectArgs, err := sq.Select(
		"request_hash",
		"response_body",
		"content_type",
		"location",
	).
		From("idempotency_keys").
		Where(sq.Eq{"idempotency_key": key}).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build idempotency key query: %w", err)
	}

	record := &idempotencyRecord{}
	err = executor.QueryRow(ctx, selectQuery, selectArgs...).Scan(
		&record.requestHash,
		&record.responseBody,
		&record.contentType,
		&record.location,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get idempotency key: %w", err)
	}

	return record, nil
}

func (store *IdempotencyStore) Save(
	ctx context.Context,
	key uuid.UUID,
	requestHash []byte,
	response *bufferedResponse,
	expiresAt time.Time,
) error {
	query, args, err := sq.Insert("idempotency_keys").
		Columns(
			"idempotency_key",
			"request_hash",
			"response_body",
			"content_type",
			"location",
			"expires_at",
		).
		Values(
			key,
			requestHash,
			response.body.Bytes(),
			response.header.Get("Content-Type"),
			response.header.Get("Location"),
			expiresAt,
		).
		PlaceholderFormat(sq.Dollar).
		ToSql()
	if err != nil {
		return fmt.Errorf("build save idempotency key query: %w", err)
	}

	if _, err = store.executor(ctx).Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("save idempotency key: %w", err)
	}

	return nil
}

func (store *IdempotencyStore) executor(ctx context.Context) databaseExecutor {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}

	return store.pool
}

func advisoryLockID(key uuid.UUID) int64 {
	return int64(binary.BigEndian.Uint64(key[:8]))
}

func IdempotencyMiddleware(
	store *IdempotencyStore,
	txManager TxManager,
	ttl time.Duration,
) api.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			if request.Method != http.MethodPost || request.URL.Path != "/api/v1/trips" {
				next.ServeHTTP(w, request)
				return
			}

			keyText := strings.TrimSpace(request.Header.Get("Idempotency-Key"))
			if keyText == "" {
				next.ServeHTTP(w, request)
				return
			}

			key, err := uuid.Parse(keyText)
			if err != nil {
				writeError(w, ErrInvalidRequest)
				return
			}

			bodyReader := http.MaxBytesReader(w, request.Body, maxIdempotentRequestBody)
			body, err := io.ReadAll(bodyReader)
			_ = bodyReader.Close()
			if err != nil {
				writeError(w, ErrInvalidRequest)
				return
			}

			requestHash := sha256.Sum256(body)
			var response *bufferedResponse
			var replay *idempotencyRecord
			conflict := false

			err = txManager.Do(request.Context(), func(txCtx context.Context) error {
				record, getErr := store.LockAndGet(txCtx, key)
				if getErr != nil {
					return getErr
				}
				if record != nil {
					if !bytes.Equal(record.requestHash, requestHash[:]) {
						conflict = true
						return nil
					}

					replay = record
					return nil
				}

				response = newBufferedResponse()
				requestCopy := request.Clone(txCtx)
				requestCopy.Body = io.NopCloser(bytes.NewReader(body))
				next.ServeHTTP(response, requestCopy)

				if response.StatusCode() != http.StatusCreated {
					return errRollbackIdempotentRequest
				}

				if saveErr := store.Save(
					txCtx,
					key,
					requestHash[:],
					response,
					time.Now().UTC().Add(ttl),
				); saveErr != nil {
					return saveErr
				}

				return nil
			})

			switch {
			case errors.Is(err, errRollbackIdempotentRequest):
				response.WriteTo(w, response.StatusCode())
			case err != nil:
				writeError(w, err)
			case conflict:
				writeError(w, ErrIdempotencyConflict)
			case replay != nil:
				writeStoredResponse(w, replay)
			default:
				response.WriteTo(w, response.StatusCode())
			}
		})
	}
}

type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func newBufferedResponse() *bufferedResponse {
	return &bufferedResponse{header: make(http.Header)}
}

func (response *bufferedResponse) Header() http.Header {
	return response.header
}

func (response *bufferedResponse) WriteHeader(status int) {
	if response.status == 0 {
		response.status = status
	}
}

func (response *bufferedResponse) Write(data []byte) (int, error) {
	if response.status == 0 {
		response.status = http.StatusOK
	}

	return response.body.Write(data)
}

func (response *bufferedResponse) StatusCode() int {
	if response.status == 0 {
		return http.StatusOK
	}

	return response.status
}

func (response *bufferedResponse) WriteTo(w http.ResponseWriter, status int) {
	copyHeaders(w.Header(), response.header)
	w.WriteHeader(status)
	_, _ = w.Write(response.body.Bytes())
}

func writeStoredResponse(w http.ResponseWriter, record *idempotencyRecord) {
	if record.contentType != "" {
		w.Header().Set("Content-Type", record.contentType)
	}
	if record.location != "" {
		w.Header().Set("Location", record.location)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(record.responseBody)
}

func copyHeaders(destination, source http.Header) {
	for name, values := range source {
		for _, value := range values {
			destination.Add(name, value)
		}
	}
}
