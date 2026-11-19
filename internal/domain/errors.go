package domain

import "errors"

var (
	ErrNoSeatsAvailable = errors.New("no seats available")
	ErrSeatNotAvailable = errors.New("seat not available")
	ErrSeatNotFound     = errors.New("seat not found")
	ErrNotHolder        = errors.New("not holder")
	ErrHoldExpired      = errors.New("hold expired")
)
