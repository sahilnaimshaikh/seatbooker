CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE shows (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name           TEXT NOT NULL,
    price_paise    INT NOT NULL CHECK (price_paise >= 0),
    per_user_limit INT NOT NULL DEFAULT 4 CHECK (per_user_limit > 0),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE seats (
    show_id    UUID NOT NULL REFERENCES shows(id),
    seat_no    TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'available' CHECK (status IN ('available', 'confirmed')),
    held_by    TEXT,
    PRIMARY KEY (show_id, seat_no)
);

CREATE INDEX idx_seats_show_status ON seats (show_id, status);

CREATE TABLE reservations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key TEXT NOT NULL UNIQUE,
    show_id         UUID NOT NULL REFERENCES shows(id),
    user_id         TEXT NOT NULL,
    seats           TEXT[] NOT NULL,
    amount_paise    INT NOT NULL CHECK (amount_paise >= 0),
    status          TEXT NOT NULL CHECK (status IN ('confirmed', 'cancelled')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_reservations_user_show ON reservations (show_id, user_id);

CREATE TABLE user_show_counts (
    show_id    UUID NOT NULL REFERENCES shows(id),
    user_id    TEXT NOT NULL,
    held_count INT NOT NULL DEFAULT 0 CHECK (held_count >= 0),
    PRIMARY KEY (show_id, user_id)
);
