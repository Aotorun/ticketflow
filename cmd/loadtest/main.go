package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
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

const goroutines = 200

func main() {
	cfg := config.Load()

	ctx := context.Background()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("postgres connect: %v", err)
	}
	defer pool.Close()

	rdb, err := redisclient.NewClient(ctx, cfg.RedisAddr)
	if err != nil {
		log.Fatalf("redis connect: %v", err)
	}
	defer rdb.Close()

	scenario(ctx, pool, rdb, true)
	scenario(ctx, pool, rdb, false)
}

func scenario(ctx context.Context, pool *pgxpool.Pool, rdb *redis.Client, withFilter bool) {
	svc := service.NewBookingService(pool, rdb)
	repo := postgres.NewBookingPool(pool)

	venueID, err := repo.CreateVenue(ctx, "Load Arena")
	if err != nil {
		log.Fatalf("create venue: %v", err)
	}
	if err := repo.AddSeats(ctx, venueID, 3, 5); err != nil {
		log.Fatalf("add seats: %v", err)
	}

	eventID0, err := svc.CreateEvent(ctx, venueID, "Load Test 0", time.Now().Add(24*time.Hour))
	if err != nil {
		log.Fatalf("create event: %v", err)
	}

	svc.ReserveAny(ctx, eventID0, uuid.New(), 5*time.Minute)

	eventID, err := svc.CreateEvent(ctx, venueID, "Load Test", time.Now().Add(24*time.Hour))
	if err != nil {
		log.Fatalf("create event: %v", err)
	}

	if !withFilter {
		if err := rdb.Del(ctx, fmt.Sprintf("available:%s", eventID)).Err(); err != nil {
			log.Fatalf("delete counter: %v", err)
		}
	}

	before := txCount(ctx, pool)

	var success, soldOut, other atomic.Int64
	var wg sync.WaitGroup

	start := time.Now()
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
				log.Printf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	time.Sleep(20 * time.Second)
	after := txCount(ctx, pool)

	var total, distinct int64
	err = pool.QueryRow(ctx,
		"SELECT count(*), count(DISTINCT seat_id) FROM bookings WHERE event_id = $1 AND status = 'held'",
		eventID).Scan(&total, &distinct)
	if err != nil {
		log.Fatalf("verify: %v", err)
	}

	fmt.Printf("\n=== withFilter=%v ===\n", withFilter)
	fmt.Printf("success:        %d\n", success.Load())
	fmt.Printf("sold out:       %d\n", soldOut.Load())
	fmt.Printf("other errors:   %d\n", other.Load())
	fmt.Printf("elapsed:        %v\n", elapsed)
	fmt.Printf("pg transactions: %d\n", after-before)
	fmt.Printf("held bookings:  %d, distinct seats: %d\n", total, distinct)
}

func txCount(ctx context.Context, pool *pgxpool.Pool) int64 {
	var n int64
	err := pool.QueryRow(ctx,
		"SELECT xact_commit + xact_rollback FROM pg_stat_database WHERE datname = current_database()").Scan(&n)
	if err != nil {
		log.Fatalf("tx count: %v", err)
	}
	return n
}
