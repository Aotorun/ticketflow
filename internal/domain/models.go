package domain

import (
	"time"

	"github.com/google/uuid"
)

type SeatStatus string

const (
	SeatAvailable SeatStatus = "available"
	SeatHeld      SeatStatus = "held"
	SeatConfirmed SeatStatus = "confirmed"
)

type BookingStatus string

const (
	BookingHeld      BookingStatus = "held"
	BookingConfirmed BookingStatus = "confirmed"
	BookingCancelled BookingStatus = "cancelled"
	BookingExpired   BookingStatus = "expired"
)

type Venue struct {
	ID        uuid.UUID
	Name      string
	CreatedAt time.Time
}

type Seat struct {
	ID         uuid.UUID
	VenueID    uuid.UUID
	RowLabel   string
	SeatNumber int
}

type Event struct {
	ID        uuid.UUID
	VenueID   uuid.UUID
	Name      string
	StartsAt  time.Time
	CreatedAt time.Time
}

type EventSeat struct {
	EventID   uuid.UUID  `json:"event_id"`
	SeatID    uuid.UUID  `json:"seat_id"`
	Status    SeatStatus `json:"status"`
	HeldBy    *uuid.UUID `json:"held_by,omitempty"`
	HeldUntil *time.Time `json:"held_until,omitempty"`
}

type Booking struct {
	ID        uuid.UUID
	EventID   uuid.UUID
	SeatID    uuid.UUID
	UserID    uuid.UUID
	Status    BookingStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}
