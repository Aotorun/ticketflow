package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/Aotorun/ticketflow/internal/domain"
	"github.com/Aotorun/ticketflow/internal/postgres"
	"github.com/Aotorun/ticketflow/internal/redisclient"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type BookingService struct {
	repo    *postgres.BookingRepo
	cache   *redisclient.SeatMapCache
	counter *redisclient.AvailabilityCounter
}

func NewBookingService(pool *pgxpool.Pool, client *redis.Client) *BookingService {
	repo := postgres.NewBookingPool(pool)
	cache := redisclient.NewSeatMapCache(client)
	counter := redisclient.NewAvailabilityCounter(client)

	return &BookingService{repo, cache, counter}
}

func (bs *BookingService) CreateEvent(ctx context.Context, venueID uuid.UUID, name string, startsAt time.Time) (uuid.UUID, error) {
	eventID, seats, err := bs.repo.CreateEvent(ctx, venueID, name, startsAt)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create event error: %w", err)
	}

	err = bs.counter.InitCount(ctx, eventID, seats)
	if err != nil {
		log.Printf("init counter err: %v", err)
	}

	return eventID, nil
}

func (bs *BookingService) GetSeatMap(ctx context.Context, eventID uuid.UUID) ([]domain.EventSeat, error) {
	seats, found, err := bs.cache.Get(ctx, eventID)
	if err != nil {
		log.Printf("redis get error: %s", err)
	}
	if found {
		return seats, nil
	}

	seats, err = bs.repo.ListEventSeats(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("get list seats: %w", err)
	}

	if err := bs.cache.Set(ctx, eventID, seats, 30*time.Second); err != nil {
		log.Printf("redis set error: %s", err)
	}

	return seats, nil
}

func (bs *BookingService) ReserveAny(ctx context.Context, eventID, userID uuid.UUID, holdTTL time.Duration) (uuid.UUID, error) {
	decremented := false

	res, err := bs.counter.TryReserve(ctx, eventID)
	if err != nil {
		log.Printf("counter error: %v", err)
	}
	switch res {
	case redisclient.SoldOut:
		return uuid.Nil, domain.ErrNoSeatsAvailable
	case redisclient.Reserved:
		decremented = true
	}

	seatID, err := bs.repo.ReserveAnySeat(ctx, eventID, userID, holdTTL)
	if err != nil {
		if decremented && !errors.Is(err, domain.ErrNoSeatsAvailable) {
			bs.giveBack(ctx, eventID)
		}
		return uuid.Nil, err
	}

	if err := bs.cache.Invalidate(ctx, eventID); err != nil {
		log.Printf("cache invalidate error: %v", err)
	}

	return seatID, nil
}

func (bs *BookingService) giveBack(ctx context.Context, eventID uuid.UUID) {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()

	if err := bs.counter.Release(cctx, eventID, 1); err != nil {
		log.Printf("counter compensate for event %s: %v", eventID, err)
	}
}

func (bs *BookingService) ReserveSpecific(ctx context.Context, eventID, seatID, userID uuid.UUID, holdTTL time.Duration) error {
	if err := bs.repo.ReserveSpecificSeat(ctx, eventID, seatID, userID, holdTTL); err != nil {
		return err
	}

	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := bs.counter.Take(cctx, eventID); err != nil {
		log.Printf("counter decrease error: %s", err)
	}

	if err := bs.cache.Invalidate(ctx, eventID); err != nil {
		log.Printf("cache invalidate error: %s", err)
	}

	return nil
}

func (bs *BookingService) ReleaseExpired(ctx context.Context) {
	expired, err := bs.repo.ReleaseExpiredHolds(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return
		}

		log.Printf("expire error: %v", err)
		return
	}

	countExpired := 0
	for eventID, n := range expired {
		if err := bs.counter.Release(ctx, eventID, n); err != nil {
			log.Printf("counter release for event %s: %v", eventID, err)
		}
		countExpired += n
	}

	if countExpired > 0 {
		log.Printf("Expired %d seats", countExpired)
	}
}

func (bs *BookingService) Confirm(ctx context.Context, eventID, seatID, userID uuid.UUID) error {
	if err := bs.repo.ConfirmSeat(ctx, eventID, seatID, userID); err != nil {
		return err
	}

	if err := bs.cache.Invalidate(ctx, eventID); err != nil {
		log.Printf("cache invalidate error: %s", err)
	}

	return nil
}

func (bs *BookingService) Reconcile(ctx context.Context) {
	eventSeatsAvailable, err := bs.repo.CountAvailable(ctx)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("reconcile error: %v", err)
		}

		return
	}

	fixed := 0
	for eventID, seats := range eventSeatsAvailable {
		old, existed, err := bs.counter.Reset(ctx, eventID, seats)
		if err != nil {
			log.Printf("reconcile event %s: %v", eventID, err)
			continue
		}

		switch {
		case !existed:
			log.Printf("event %s: counter was missing, set to %d", eventID, seats)
			fixed++
		case old < seats:
			log.Printf("event %s: counter UNDERSTATED, was %d, set to %d", eventID, old, seats)
			fixed++
		case old > seats:
			log.Printf("event %s: counter overstated, was %d, set to %d", eventID, old, seats)
			fixed++
		}
	}

	if fixed > 0 {
		log.Printf("reconcile fixed %d events", fixed)
	}
}
