# Seatbooking

A PostgreSQL-backed JSON API for assigned-seat reservations. The project focuses on correct reservation decisions under concurrent requests: a seat is confirmed at most once, per-user limits are enforced, and retries are idempotent.

## Current status

The API, migrations, health checks, Prometheus metrics, and burst test are implemented. Public hosting and the live URL are not configured yet. The selected deployment direction is a Render web service with PostgreSQL managed separately (Neon was discussed, but a database URL must be configured before deploying).

## Requirements

- Go 1.25 or later for running locally
- PostgreSQL 16 or compatible, started separately from this Compose project
- Docker with the Compose plugin for containerized local runs

## Configuration

The app reads configuration from environment variables; Go does not load `.env` files itself. Create a `.env` file in the repository root and load it into your shell for local `go run` commands. Compose loads `.env` automatically. Do not commit `.env`.

```dotenv
DATABASE_URL=postgres://USER:PASSWORD@HOST:5432/DATABASE?sslmode=require
JWT_SECRET=replace-with-a-long-random-secret
ADMIN_TOKEN=replace-with-a-long-random-token
APP_PORT=8080
LOG_LEVEL=info
ENVIRONMENT=dev
DB_MAX_OPEN_CONNS=20
DB_MAX_IDLE_CONNS=10
```

`DATABASE_URL`, `JWT_SECRET`, and `ADMIN_TOKEN` are required. Other variables are optional. `MIGRATIONS_FOLDER` can point to SQL migrations on disk; if it is unset, the migrations embedded in the binary are used.

For PostgreSQL running on the Mac host while the API runs in Docker, use `host.docker.internal` as the database host. For a remote managed PostgreSQL database, use the provider's direct (non-pooled) connection URL and required TLS options.

## Run locally

Start PostgreSQL separately, configure `.env`, then run the migration command once before serving:

```sh
set -a
source .env
set +a
go run ./cmd/seatbooking migrate
go run ./cmd/seatbooking serve
```

The server listens on `APP_PORT` (default `8080`). `serve` does not run migrations; `migrate` applies pending versions and exits successfully when the schema is already current.

## Run with Docker Compose

Compose does not start PostgreSQL. It expects the database in `DATABASE_URL` to be running and reachable from the containers.

```sh
docker compose up --build
```

Compose runs the `migrate` service first and starts the API only if migration succeeds. The API is available on `http://localhost:8080` by default. To stop the stack, press Ctrl+C; the separately managed database is not stopped or removed.

## API

Successful and error responses use the JSON envelope `{"data": ...}` or `{"error":{"code":"...","message":"..."}}`.

| Method and path                  | Purpose                                  | Authentication                       | Success        |
| -------------------------------- | ---------------------------------------- | ------------------------------------ | -------------- |
| `POST /auth/login`               | Issue a test JWT for a user ID           | None                                 | `200`          |
| `POST /shows`                    | Create a show and its seats              | `Authorization: Bearer $ADMIN_TOKEN` | `201`          |
| `GET /shows/{id}`                | Read show, seats, and counts             | None                                 | `200`          |
| `POST /shows/{id}/reserve`       | Reserve one or more seats                | User JWT bearer token                | `201`          |
| `POST /reservations/{id}/cancel` | Cancel the owner's confirmed reservation | User JWT bearer token                | `200`          |
| `GET /health`                    | Liveness check                           | None                                 | `200`          |
| `GET /ready`                     | Check PostgreSQL readiness               | None                                 | `200` or `503` |
| `GET /metrics`                   | Prometheus exposition                    | None                                 | `200`          |

### Authentication

`POST /auth/login` accepts `{"user_id":"buyer-1"}` and returns a signed, expiring HS256 JWT. The reservation identity comes from the token subject, not the reservation body. This endpoint is a test identity issuer and does not authenticate real accounts; do not use it as production login.

Show creation uses the configured admin bearer token. Reserve requests take their idempotency key from the JSON body:

```json
{ "seats": ["A12"], "idempotency_key": "unique-key-for-this-action" }
```

### Reservation behavior

- Money is an integer number of paise.
- Seat lists are sorted before locking. The service locks the user's show-count row and requested seat rows inside one PostgreSQL transaction.
- Multi-seat requests are **all-or-nothing**: if any requested seat is missing, taken, or the request exceeds the user limit, the transaction does not partially confirm seats.
- Duplicate seat numbers in one request are rejected with `400`.
- The reservation table's unique idempotency-key constraint ensures the same request returns the original reservation. Reusing the key with a different user, show, or seat list returns `409`.
- A reservation is confirmed immediately. Release uses owner-only cancellation; there is no temporary `held` state or expiry workflow. Show reconciliation for this model is `available + confirmed = total_seats`.
- Expected conflicts such as seat-taken, per-user-limit, idempotency mismatch, and lock timeout return `409`; invalid input returns `400`, missing resources return `404`, and unauthorized requests return `401`.

### Test login example

```sh
curl -sS -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"user_id":"buyer-1"}'
```

Use the returned `data.token` as `Authorization: Bearer <token>` for reserve/cancel requests.

### Create a show

```sh
curl -sS -X POST http://localhost:8080/shows \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"friday-night","seats":["A1","A2","A3"],"price_paise":25000}'
```

If `per_user_limit` is omitted, it defaults to `4`. The response contains the generated show ID and every seat in `available` state.

### Read show state

```sh
SHOW_ID='paste-the-id-from-the-create-show-response'
curl -sS "http://localhost:8080/shows/$SHOW_ID"
```

The response includes each seat and the `available`, `confirmed`, and `total_seats` counts. The chosen model confirms immediately and supports cancellation, so it has no temporary `held` state; its invariant is `available + confirmed = total_seats`.

### Reserve and cancel

Use the JWT returned by `/auth/login`:

```sh
SHOW_ID='paste-the-id-from-the-create-show-response'
USER_TOKEN='paste-the-token-from-the-login-response'
curl -sS -X POST "http://localhost:8080/shows/$SHOW_ID/reserve" \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"seats":["A1"],"idempotency_key":"unique-key-for-this-action"}'

RESERVATION_ID='paste-the-reservation_id-from-the-reserve-response'
curl -sS -X POST "http://localhost:8080/reservations/$RESERVATION_ID/cancel" \
  -H "Authorization: Bearer $USER_TOKEN"
```

Reservation success is `201`; a seat conflict, user-limit decline, or reused key with a different request is `409`. Cancellation is owner-only.

## Health, metrics, and logs

- `/health` is a liveness endpoint and does not depend on PostgreSQL.
- `/ready` pings PostgreSQL with a bounded timeout and returns `503` if it cannot reach the database.
- `/metrics` exposes:
  - `seatbooking_reservations_confirmed_total`
  - `seatbooking_reservations_declined_total{reason="seat-taken"}`
  - `seatbooking_reservations_declined_total{reason="per-user-limit"}`
  - `seatbooking_reservations_declined_total{reason="idempotent-replay"}`
  - `seatbooking_reservations_declined_total{reason="lock-timeout"}`
  - `seatbooking_seats_available{show_id="..."}`
- Idempotent replays return the original successful `201` response; the metrics implementation records them under the requested `idempotent-replay` reason and does not count them as a new confirmation.
- Structured request logs go to stdout and include the request ID, method, path, status, and latency. The platform/container runtime captures stdout; public log access depends on the hosting provider.

## Tests

Run unit and package tests:

```sh
go test ./...
```

PostgreSQL integration tests are opt-in. They use a direct database connection, create a uniquely named schema, apply the embedded migration, run the API against that schema, and drop it afterward. They also ensure the `pgcrypto` extension is available. The database user needs permission to create schemas and install/use that extension.

```sh
TEST_DATABASE_URL='postgres://USER:PASSWORD@HOST:5432/DATABASE?sslmode=require' go test ./pkg/integration
```

The integration suite defaults to a 500-request hot-seat storm. Set `SEATBOOKING_STORM_SIZE=20000` to run the requirements-scale version:

```sh
TEST_DATABASE_URL='postgres://USER:PASSWORD@HOST:5432/DATABASE?sslmode=require' \
SEATBOOKING_STORM_SIZE=20000 \
go test -run TestHotSeatStormHasOneWinnerAndMaintainsReconciliation ./pkg/integration
```

## Burst script

`burst.sh` hits a running API through its public HTTP endpoints. It creates three uniquely named shows and leaves their shows/reservations in the target database; use a test database, not production.

For a local server, load `.env` and run:

```sh
set -a
source .env
set +a
./burst.sh http://localhost:8080
```

The default hot-seat storm is 20,000 requests. The script has been run locally at that size. `BURST_SIZE` changes that number and `BURST_CONNECTIONS` controls the maximum concurrent HTTP connections (default `1000`). The script also checks the per-user limit, retries the same idempotency key, tries that key with a different body, and compares final API state with the availability gauge.

A non-local URL is rejected by default because the script writes data. Set `ALLOW_REMOTE_BURST=true` only when you intentionally want to run it against a remote test deployment. The target must also use the matching `ADMIN_TOKEN` and `JWT_SECRET` in the environment.

## Deployment status

Render is the intended API host and PostgreSQL is intended to remain separately managed. Deployment is not configured yet and there is no live URL. On Render's free web-service plan, migrations must be run separately against the managed database before deployment; paid plans can use a pre-deploy migration command. Configure `APP_PORT` to the port Render provides, set `DATABASE_URL`, `JWT_SECRET`, and `ADMIN_TOKEN` as secrets, and set the health-check path to `/ready`. The repository must be pushed to a public Git provider before Render can build it.

