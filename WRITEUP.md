# Seatbooking Write-up

## Atomic reservation decision

PostgreSQL is the source of truth for shows, seats, reservations, and each user's confirmed-seat count. The money amount is stored and returned as integer paise; no floating-point arithmetic is used.

`ReservationService.Reserve` makes the decision inside one SQL transaction. It sorts requested seat numbers, claims the idempotency key with the reservations table's unique constraint, creates and locks the `(show_id, user_id)` count row, checks the per-user limit, then locks requested seat rows with `SELECT ... FOR UPDATE` in seat-number order. It confirms the seats with a conditional update that only changes rows still marked `available`, increments the user's count, and commits. Any expected decline or database error before commit rolls back the transaction.

The sorted seat order gives overlapping multi-seat requests the same lock order, avoiding the classic A-then-B versus B-then-A deadlock. A two-second PostgreSQL lock timeout bounds lock waiting; PostgreSQL SQLSTATE `55P03` is translated to a `409` conflict.

Multi-seat requests are all-or-nothing. If any requested seat is missing or unavailable, or the user would exceed the limit, the transaction is not committed and none of those requested seats are confirmed. The HTTP handler rejects duplicate seat numbers before calling the service.

## Idempotency

`reservations.idempotency_key` is globally unique. The service checks for an existing key before enforcing the user's limit. If two identical requests race, the unique constraint serializes their insert attempts. The winner commits the reservation; the retry reads and returns that original reservation. The retry is marked internally as replayed and does not create another reservation or increment confirmed seats.

A key reused for a different user, show, or sorted seat list returns a domain conflict, mapped to HTTP `409`. The key is accepted from the request body only. A request without a key is invalid.

## Cancellation and seat states

This project uses immediate confirmation with explicit owner-only cancellation, not temporary holds or timed expiry. A successful reservation changes seats from `available` to `confirmed`; cancellation changes the reservation to `cancelled`, returns only seats still confirmed for that owner to `available`, and decrements the user's count in the same transaction. A repeated or non-owner cancellation does not release seats owned by someone else.

There is no `held` state in this model. Show reconciliation is `available + confirmed = total_seats`.

## Consistency and availability

PostgreSQL commits are authoritative. The service does not report a reservation as successful until the seat updates, reservation row, and user count commit together. If PostgreSQL cannot be reached, the service cannot confirm a booking; this favors consistency over accepting writes during a partition.

Readiness pings PostgreSQL and returns `503` when it cannot reach the database. Expected seat, limit, idempotency, and lock-timeout outcomes return `409`. One limitation remains: unexpected database/network errors from reservation handlers currently fall through to the generic `500` mapping. Improving infrastructure-error classification (for example, `503` with retry guidance) is follow-up work; the current 20k local burst test is not a proof of behavior during a database outage.

## Observability

Structured JSON logs go to stdout. The logging middleware records request ID, method, path, status, latency, and a user-ID field. The router currently places logging outside authentication, so the user-ID field may be empty for authenticated requests; moving it inside or enriching the logging context is a follow-up.

`GET /metrics` exposes:

- `seatbooking_reservations_confirmed_total` for newly committed reservations.
- `seatbooking_reservations_declined_total{reason=...}` for `seat-taken`, `per-user-limit`, `idempotent-replay`, and `lock-timeout` outcomes. The `idempotent-replay` label includes successful retries that return `201`, as well as same-key/different-body conflicts; those retries are not new confirmations.
- `seatbooking_seats_available{show_id=...}`, read from PostgreSQL when Prometheus scrapes the endpoint.

For an on-call alert, I would page on readiness failures, any unexpected HTTP 5xx, sustained request-latency or database-pool saturation, and persistent discrepancies between API seat counts and the availability gauge. Seat-taken declines are expected during a hot-seat storm; a sudden change in their rate is useful context, not automatically an incident.

## AI usage

AI tools were used to explain the requirements, discuss PostgreSQL locking and idempotency, draft and revise Go handlers/services/router code, and create integration and burst-test scaffolding. The developer directed the key decisions: PostgreSQL as the transactional source of truth, sorted row locking, explicit cancellation instead of timed holds, body-only idempotency keys, package-level handler factories, separate `migrate` and `serve` commands, and the reference repository's code organization. The developer reviewed changes incrementally and made final workflow and hosting choices. AI assistance does not replace understanding or ownership of the code.

## Verification and next steps

The PostgreSQL integration suite creates a uniquely named schema, applies the embedded migration, and drops that schema after each test. It covers a hot-seat storm, concurrent per-user limits, idempotent retries, and API/gauge reconciliation. A local 20,000-request hot-seat run produced one confirmation, 19,999 seat-taken declines, zero 5xx/network errors, and reconciled final state. This does not prove performance on the eventual hosted plan or behavior during a database outage.

The burst script calls the real HTTP endpoints and reports confirmations, replays, decline reasons, 5xx/transport errors, and final reconciliation. It creates shows and reservations that remain in the target database, so it must be run against a test database. Remote runs require explicit opt-in.

Remaining work is to publish the Git repository, connect the separately managed PostgreSQL database, deploy the API (Render is the current choice), run migrations before deployment, provide the live URL and metrics/log access, and document the deployment-specific commands once configured. A further code cleanup is to route Prometheus metrics through the service/handler layers and make request logs include authenticated user IDs.
