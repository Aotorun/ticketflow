CREATE EXTENSION IF NOT EXISTS "pgcrypto"; -- для gen_random_uuid()

CREATE TABLE venues (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE seats (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    venue_id UUID NOT NULL REFERENCES venues(id),
    row_label TEXT NOT NULL,
    seat_number INT NOT NULL,
    UNIQUE (venue_id, row_label, seat_number)
);

CREATE TABLE events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    venue_id UUID NOT NULL REFERENCES venues(id),
    name TEXT NOT NULL,
    starts_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- "горячая" таблица — по одной строке на (event, seat), именно её лочим
CREATE TABLE event_seats (
    event_id UUID NOT NULL REFERENCES events(id),
    seat_id UUID NOT NULL REFERENCES seats(id),
    status TEXT NOT NULL DEFAULT 'available'
        CHECK (status IN ('available', 'held', 'confirmed')),
    held_by UUID,
    held_until TIMESTAMPTZ,
    PRIMARY KEY (event_id, seat_id)
);

-- история бронирований (аудит), не участвует в блокировках
CREATE TABLE bookings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id UUID NOT NULL REFERENCES events(id),
    seat_id UUID NOT NULL REFERENCES seats(id),
    user_id UUID NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('held', 'confirmed', 'cancelled', 'expired')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_bookings_event_seat ON bookings(event_id, seat_id);
CREATE INDEX idx_event_seats_status ON event_seats(event_id, status);