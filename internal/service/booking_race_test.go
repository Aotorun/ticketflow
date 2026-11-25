package service_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Aotorun/ticketflow/internal/config"
	"github.com/Aotorun/ticketflow/internal/domain"
	"github.com/Aotorun/ticketflow/internal/postgres"
	"github.com/Aotorun/ticketflow/internal/redisclient"
	"github.com/Aotorun/ticketflow/internal/service"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func setup(t *testing.T) (*service.BookingService, *postgres.BookingRepo, *pgxpool.Pool, *redis.Client) {
	t.Helper()
	cfg := config.Load()

	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Skipf("no connection to posgres")
	}
	t.Log("postgres connected")

	rdb, err := redisclient.NewClient(ctx, cfg.RedisAddr)
	if err != nil {
		pool.Close()
		t.Skipf("no connection to redis")
	}
	t.Log("redis connected")

	repo := postgres.NewBookingPool(pool)

	t.Cleanup(func() {
		pool.Close()
		rdb.Close()
	})

	svc := service.NewBookingService(pool, rdb)

	return svc, repo, pool, rdb
}

func TestReserveAnyRace(t *testing.T) {
	svc, repo, pool, rdb := setup(t)

	ctx := context.Background()
	venueID, err := repo.CreateVenue(ctx, "Race Arena")
	if err != nil {
		t.Fatalf("create venue: %v", err)
	}
	err = repo.AddSeats(ctx, venueID, 2, 5)
	if err != nil {
		t.Fatalf("add seats: %v", err)
	}

	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, "DELETE FROM seats WHERE venue_id = $1", venueID); err != nil {
			t.Logf("cleanup: %v", err)
		}
		if _, err := pool.Exec(ctx, "DELETE FROM venues WHERE id = $1", venueID); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	eventID := newEvent(t, ctx, svc, pool, venueID, "Race test 1")
	eventID2 := newEvent(t, ctx, svc, pool, venueID, "Race test 2")

	t.Run("with counter", func(t *testing.T) {
		runRace(t, ctx, svc, pool, eventID, 50, 10)
	})

	t.Run("without counter", func(t *testing.T) {
		rdb.Del(ctx, "available:"+eventID2.String())

		runRace(t, ctx, svc, pool, eventID2, 50, 10)
	})
}

func runRace(t *testing.T, ctx context.Context, svc *service.BookingService, pool *pgxpool.Pool, eventID uuid.UUID, goroutines, wantSeats int) {
	t.Helper()
	var success, soldOut, other atomic.Int64
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			_, err := svc.ReserveAny(ctx, eventID, uuid.New(), 5*time.Minute)
			switch {
			case err == nil:
				success.Add(1)
			case errors.Is(err, domain.ErrNoSeatsAvailable):
				soldOut.Add(1)
			default:
				other.Add(1)
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	if success.Load() != int64(wantSeats) {
		t.Errorf("success want %v, got %v", wantSeats, success.Load())
	}
	if soldOut.Load() != int64(goroutines-wantSeats) {
		t.Errorf("soldOut want %v, got %v", goroutines-wantSeats, soldOut.Load())
	}
	if other.Load() != 0 {
		t.Errorf("other want %v, got %v", 0, other.Load())
	}

	var total, distinct int64
	err := pool.QueryRow(ctx,
		"SELECT count(*), count(DISTINCT seat_id) FROM bookings WHERE event_id = $1 AND status = 'held'",
		eventID).Scan(&total, &distinct)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	if total != int64(wantSeats) {
		t.Errorf("total want %v, got %v", wantSeats, total)
	}
	if distinct != int64(wantSeats) {
		t.Errorf("distinct want %v, got %v", wantSeats, distinct)
	}
}

func newEvent(t *testing.T, ctx context.Context, svc *service.BookingService, pool *pgxpool.Pool, venueID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	eventID, err := svc.CreateEvent(ctx, venueID, name, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, "DELETE FROM bookings WHERE event_id = $1", eventID); err != nil {
			t.Logf("cleanup: %v", err)
		}
		if _, err := pool.Exec(ctx, "DELETE FROM event_seats WHERE event_id = $1", eventID); err != nil {
			t.Logf("cleanup: %v", err)
		}
		if _, err := pool.Exec(ctx, "DELETE FROM events WHERE id = $1", eventID); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	return eventID
}
