# Seatbooking Write-up

## Atomic reservation decision

PostgreSQL is the source of truth for shows, seats, reservations, and each user's confirmed-seat count. Money is stored and returned as integer paise; there is no floating-point arithmetic.

The exact reservation decision is in `ReservationService.Reserve`, inside one SQL transaction:

1. Sort the requested seat numbers in Go.
2. Look up the idempotency key. If it exists, compare the original user, show, and normalized seat list; return the existing reservation only if all match.
3. Read the show to obtain its price and per-user limit.
4. Insert the reservation row. The database has a global `UNIQUE` constraint on `reservations.idempotency_key`, which arbitrates simultaneous requests using the same key.
5. Create the `(show_id, user_id)` count row if needed, then select it `FOR UPDATE`. This serializes limit checks for the same user and show.
6. Select requested seats `ORDER BY seat_no FOR UPDATE`, verify every seat is `available`, then update with `WHERE status = 'available'`. The code verifies the affected-row count.
7. Increment the user's count and commit. Any earlier return rolls back the transaction.

The unique constraint prevents two transactions from claiming the same idempotency key. The seat row locks and conditional update ensure only a transaction that still sees an available seat can confirm it. A two-second PostgreSQL `lock_timeout` bounds waits; SQLSTATE `55P03` becomes a `409` conflict.

For competing **reservation** transactions, sorting seats gives every multi-seat request the same lock order, avoiding the A-then-B/B-then-A deadlock pattern. Requests are all-or-nothing: a missing/taken seat or over-limit request rolls back without confirming any requested seats. The handler rejects duplicate seat numbers before calling the service.

Cancellation follows the same high-level order: it first reads the reservation's show ID without locking it, locks that user's `(show_id, user_id)` count row, conditionally updates the reservation, sorts and locks its seat rows, then releases them and decrements the count. That count-row lock serializes cancel and reserve for the same user/show; sorting seats gives overlapping operations a consistent seat-lock order. The PostgreSQL integration test races cancellation against a same-user rebooking 30 times and passed against a disposable local PostgreSQL instance.

## Idempotency

The key is stored in `reservations.idempotency_key`, which is globally unique. It is checked before the user limit. When two requests race and both initially see no row, the unique index makes the losing insert wait. If the first transaction commits, the loser rolls back and reads the committed reservation outside that failed transaction. If the first transaction rolls back, the other insert can proceed.

A replay must match the original `user_id`, `show_id`, and sorted seat list. A match returns the original reservation ID and response; it does not confirm seats or increment the user's count again. Reusing the key with a different user, show, or seat list returns `409`. The HTTP API reads the key from the JSON body only; a missing key is invalid input. The burst test verifies 50 concurrent identical attempts share one reservation ID and that same-key/different-seat input gets `409`.

## Cancellation and seat states

The selected model is immediate confirmation with explicit owner-only cancellation, not temporary holds or timed expiry. Successful requests move seats from `available` to `confirmed`. Cancellation uses a conditional reservation update scoped to the ID, owner, and `confirmed` status; it then releases seats only when they are still `confirmed` for that same owner, decrements the count, and commits those changes together. A repeated or non-owner cancellation cannot release another user's seat.

There is no temporary `held` state. The reconciliation invariant for this model is `available + confirmed = total_seats`. The schema's `user_show_counts.held_count` name is historical; it tracks currently confirmed seats and is decremented on cancellation.

## Consistency and availability

PostgreSQL is the single write authority; there is no Redis lock or second source of truth. The service does not report a new reservation as successful until the seat state, reservation row, and user count commit together. During a network partition or DB outage, booking cannot safely continue, so the system favors consistency over accepting writes; readiness returns `503` when PostgreSQL cannot be pinged.

Expected seat-taken, per-user-limit, idempotency-conflict, and lock-timeout outcomes map to `409`. Unexpected database/network errors currently fall through to generic `500`; classifying transient infrastructure errors as `503` is follow-up work. The local 20k test demonstrates behavior with the tested local PostgreSQL setup, not during a partition or on the eventual hosted database.

## Observability

Structured JSON logs go to stdout. The middleware records request ID, method, path, status, latency, and a user-ID field. The router currently places logging outside authentication, so the user-ID field may be empty for authenticated requests; moving/enriching the logging context is follow-up work. Render captures stdout in the [service Logs page](https://dashboard.render.com/web/srv-davpm33ncjis73f9t2ag/logs); this requires Render dashboard access and is not a public log endpoint.

The deployed API is at [https://seatbooking-api.onrender.com](https://seatbooking-api.onrender.com), and its public Prometheus endpoint is [https://seatbooking-api.onrender.com/metrics](https://seatbooking-api.onrender.com/metrics). The endpoint was verified with HTTP `200` after deployment. Counter-vector series are created when their reason label is first used, and the availability gauge emits one series per existing show.

`GET /metrics` exposes:

- `seatbooking_reservations_confirmed_total` for newly committed reservations.
- `seatbooking_reservations_declined_total{reason=...}` for `seat-taken`, `per-user-limit`, `idempotent-replay`, and `lock-timeout` outcomes. The `idempotent-replay` label includes successful retries that return `201`, as well as same-key/different-body conflicts; those retries are not new confirmations.
- `seatbooking_seats_available{show_id=...}`, read from PostgreSQL when Prometheus scrapes the endpoint.

The availability collector currently queries the DB directly from the metrics package, and reservation counters are incremented in the HTTP handler. This works but does not follow the desired handler/service/database layering; moving metric recording behind service outcomes and serving the exposition through a handler factory is follow-up work.

For a 2 a.m. page, alert on sustained readiness failures, any unexpected 5xx, rising latency or DB-pool wait/saturation, repeated lock timeouts, and persistent disagreement between `/shows/{id}` and the availability gauge. Seat-taken declines are expected during an on-sale storm and are not, by themselves, an incident. Same-key/different-body conflicts may indicate client key reuse errors.

## AI usage

AI assistance was used to explain the requirements and database behavior, propose designs, draft/refine Go code and tests, debug failures, and draft documentation. The developer chose PostgreSQL without Redis, explicit owner-only cancellation instead of timed holds, body-only idempotency keys, the handler-factory/router style, separate `migrate`/`serve` commands, and the independent database/hosting direction. The developer reviewed changes incrementally, corrected the workflow when it diverged from the reference repo, and ran the local PostgreSQL/concurrency checks. AI-generated code was not treated as verified until built/tested; the implementation and its limitations remain the developer's responsibility.

## Verification and next steps

The PostgreSQL integration suite uses `TEST_DATABASE_URL`, creates a uniquely named schema, applies the embedded migration, and drops only that schema afterward. It covers a hot-seat storm, concurrent per-user limits, identical idempotent retries, same-key/different-body conflict, API/gauge reconciliation, and concurrent cancellation/rebooking. Against a disposable local PostgreSQL 16 container, the 20,000-request hot-seat run produced one confirmation, 19,999 `409` seat-taken declines, zero 5xx/network errors, and reconciled final state. The cancel/rebook race test also passed 30 iterations. The same 20k burst succeeded through `burst.sh`. This is local evidence, not proof for the eventual hosted resource limits or database outage behavior.

The burst script calls the real HTTP endpoints and reports confirmations, replays, decline reasons, 5xx/transport errors, and final reconciliation. It creates shows and reservations that remain in the target database, so it must be run against a test database. Remote runs require explicit opt-in.

Remaining work: finish the metrics/service/handler layering and authenticated log context, and classify transient DB errors as `503`. The Render service and Neon connection are live; the container startup applies migrations before serving. The metrics endpoint is public, while application logs require Render dashboard access. The test-token login is intentionally challenge-only and accepts an arbitrary user ID; it must not be treated as production authentication.
