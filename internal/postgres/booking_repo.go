package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/Aotorun/ticketflow/internal/domain"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type BookingRepo struct {
	pool *pgxpool.Pool
}

func NewBookingPool(pool *pgxpool.Pool) *BookingRepo {
	return &BookingRepo{pool}
}

func (b BookingRepo) CreateVenue(ctx context.Context, name string) (uuid.UUID, error) {
	row := b.pool.QueryRow(ctx, "INSERT INTO venues (name) VALUES ($1) RETURNING id", name)

	var venueID uuid.UUID
	err := row.Scan(&venueID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("cannot read venue id: %w", err)
	}

	return venueID, nil
}

func (b BookingRepo) AddSeats(ctx context.Context, venueID uuid.UUID, rows, seatsPerRows int) error {
	rowLabels := make([]string, 0, rows*seatsPerRows)
	seatNumbers := make([]int32, 0, rows*seatsPerRows)

	for i := 0; i < rows; i++ {
		for j := 1; j <= seatsPerRows; j++ {
			rowLabels = append(rowLabels, string(rune('A'+i)))
			seatNumbers = append(seatNumbers, int32(j))
		}
	}

	query := `
		INSERT INTO seats (venue_id, row_label, seat_number)
		SELECT $1, unnest($2::text[]), unnest($3::int[])`

	if _, err := b.pool.Exec(ctx, query, venueID, rowLabels, seatNumbers); err != nil {
		return fmt.Errorf("insert seats: %w", err)
	}
	return nil
}

func (b BookingRepo) CreateEvent(ctx context.Context, venueID uuid.UUID, name string, startsAt time.Time) (uuid.UUID, int, error) {
	tx, err := b.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, 0, fmt.Errorf("cannot begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, "INSERT INTO events (venue_id, name, starts_at) VALUES ($1, $2, $3) RETURNING id", venueID, name, startsAt)
	var eventID uuid.UUID
	err = row.Scan(&eventID)
	if err != nil {
		return uuid.Nil, 0, fmt.Errorf("cannot read event id: %w", err)
	}

	tag, err := tx.Exec(ctx,
		`INSERT INTO event_seats (event_id, seat_id) 
		SELECT $1, id 
		FROM seats WHERE venue_id = $2`,
		eventID, venueID)
	if err != nil {
		return uuid.Nil, 0, fmt.Errorf("cannot get available seats: %w", err)
	}

	return eventID, int(tag.RowsAffected()), tx.Commit(ctx)
}

func (b BookingRepo) ReserveAnySeat(ctx context.Context, eventID, userID uuid.UUID, holdTTL time.Duration) (uuid.UUID, error) {
	tx, err := b.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("cannot begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, "SELECT seat_id FROM event_seats WHERE event_id = $1 AND status = 'available' FOR UPDATE SKIP LOCKED LIMIT 1", eventID)
	var seatForReserve uuid.UUID
	err = row.Scan(&seatForReserve)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, domain.ErrNoSeatsAvailable
		}

		return uuid.Nil, fmt.Errorf("cannot select free seat: %w", err)
	}

	_, err = tx.Exec(ctx, "UPDATE event_seats SET status = 'held', held_by = $1, held_until = $2 WHERE event_id = $3 AND seat_id = $4", userID, time.Now().Add(holdTTL), eventID, seatForReserve)
	if err != nil {
		return uuid.Nil, fmt.Errorf("cannot change status: %w", err)
	}

	_, err = tx.Exec(ctx, "INSERT INTO bookings (event_id, seat_id, user_id, status) VALUES ($1, $2, $3, 'held')", eventID, seatForReserve, userID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("cannot insert booking: %w", err)
	}

	return seatForReserve, tx.Commit(ctx)
}

func (b BookingRepo) ReserveSpecificSeat(ctx context.Context, eventID, seatID, userID uuid.UUID, holdTTL time.Duration) error {
	tx, err := b.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("cannot begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, "SELECT status FROM event_seats WHERE event_id = $1 AND seat_id = $2 FOR UPDATE", eventID, seatID)
	var status string
	err = row.Scan(&status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrSeatNotFound
		}

		return fmt.Errorf("cannot read status: %w", err)
	}

	if status != "available" {
		return domain.ErrSeatNotAvailable
	}

	_, err = tx.Exec(ctx, "UPDATE event_seats SET status = 'held', held_by = $1, held_until = $2 WHERE event_id = $3 AND seat_id = $4", userID, time.Now().Add(holdTTL), eventID, seatID)
	if err != nil {
		return fmt.Errorf("cannot change status: %w", err)
	}

	_, err = tx.Exec(ctx, "INSERT INTO bookings (event_id, seat_id, user_id, status) VALUES ($1, $2, $3, 'held')", eventID, seatID, userID)
	if err != nil {
		return fmt.Errorf("cannot insert booking: %w", err)
	}

	return tx.Commit(ctx)
}

func (b BookingRepo) ReleaseExpiredHolds(ctx context.Context) (map[uuid.UUID]int, error) {
	query := `	WITH released AS (
   					UPDATE event_seats
    				SET status = 'available', held_by = NULL, held_until = NULL
    				WHERE status = 'held' AND held_until < now()
    				RETURNING event_id, seat_id
					)
				UPDATE bookings b
				SET status = 'expired', updated_at = now()
				FROM released r
				WHERE b.event_id = r.event_id AND b.seat_id = r.seat_id AND b.status = 'held'
				RETURNING b.event_id, b.seat_id`

	rows, err := b.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("connot change status: %w", err)
	}
	defer rows.Close()

	m := make(map[uuid.UUID]int)
	var eventID, seatID uuid.UUID
	for rows.Next() {
		if err = rows.Scan(&eventID, &seatID); err != nil {
			return nil, fmt.Errorf("scan error: %w", err)
		}
		m[eventID]++
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return m, nil
}

func (b BookingRepo) ListEventSeats(ctx context.Context, eventID uuid.UUID) ([]domain.EventSeat, error) {
	rows, err := b.pool.Query(ctx, "SELECT event_id, seat_id, status, held_by, held_until FROM event_seats WHERE event_id = $1", eventID)
	if err != nil {
		return nil, fmt.Errorf("cannot get list seats for event:%s :%w", eventID, err)
	}
	defer rows.Close()

	seats := make([]domain.EventSeat, 0)
	for rows.Next() {
		seat := domain.EventSeat{}
		if err := rows.Scan(&seat.EventID, &seat.SeatID, &seat.Status, &seat.HeldBy, &seat.HeldUntil); err != nil {
			return nil, fmt.Errorf("scan event seat: %w", err)
		}
		seats = append(seats, seat)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("cannot read rows: %w", err)
	}

	return seats, nil
}

func (b BookingRepo) ConfirmSeat(ctx context.Context, eventID, seatID, userID uuid.UUID) error {
	tx, err := b.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("cannot begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var (
		status    string
		heldBy    *uuid.UUID
		heldUntil *bool
	)

	row := tx.QueryRow(ctx, "SELECT status, held_by, held_until <= now() FROM event_seats WHERE event_id = $1 AND seat_id = $2 FOR UPDATE", eventID, seatID)
	err = row.Scan(&status, &heldBy, &heldUntil)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrSeatNotFound
		}

		return fmt.Errorf("cannot read row: %w", err)
	}

	if status != "held" {
		return domain.ErrSeatNotAvailable
	}

	if heldBy == nil || *heldBy != userID {
		return domain.ErrNotHolder
	}

	if heldUntil == nil || *heldUntil {
		return domain.ErrHoldExpired
	}

	_, err = tx.Exec(ctx, "UPDATE event_seats SET status = 'confirmed', held_until = NULL WHERE event_id = $1 AND seat_id = $2", eventID, seatID)
	if err != nil {
		return fmt.Errorf("update event_seats: %w", err)
	}

	tag, err := tx.Exec(ctx, "UPDATE bookings SET status = 'confirmed', updated_at = now() WHERE event_id = $1 AND seat_id = $2 AND user_id = $3 AND status = 'held'", eventID, seatID, userID)
	if err != nil {
		return fmt.Errorf("update bookings: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("bookings out of sync for seat %s: affected %d rows", seatID, tag.RowsAffected())
	}

	return tx.Commit(ctx)
}

func (b BookingRepo) CountAvailable(ctx context.Context) (map[uuid.UUID]int, error) {
	rows, err := b.pool.Query(ctx, "SELECT es.event_id, count(*) FILTER (WHERE es.status = 'available') FROM event_seats es JOIN events e ON e.id = es.event_id WHERE e.starts_at > now() GROUP BY es.event_id")
	if err != nil {
		return nil, fmt.Errorf("find available seats error: %w", err)
	}
	defer rows.Close()

	eventSeatsAvailable := make(map[uuid.UUID]int)
	for rows.Next() {
		var eventID uuid.UUID
		var seats int64
		if err := rows.Scan(&eventID, &seats); err != nil {
			return nil, fmt.Errorf("scan available seats error: %w", err)
		}
		eventSeatsAvailable[eventID] = int(seats)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("cannot read rows: %w", err)
	}

	return eventSeatsAvailable, nil
}
