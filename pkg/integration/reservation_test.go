package integration_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/rs/zerolog"

	"github.com/paytm-hack/seatbooking/migrations"
	"github.com/paytm-hack/seatbooking/pkg/config"
	"github.com/paytm-hack/seatbooking/pkg/db"
	"github.com/paytm-hack/seatbooking/pkg/logctx"
	"github.com/paytm-hack/seatbooking/pkg/metrics"
	"github.com/paytm-hack/seatbooking/pkg/router"
	"github.com/paytm-hack/seatbooking/pkg/service"
)

const (
	integrationJWTSecret = "integration-test-secret"
	integrationAdminKey  = "integration-test-admin"
)

type integrationApp struct {
	client     *http.Client
	server     *httptest.Server
	showID     string
	testSchema string
	database   *sql.DB
	adminDB    *sql.DB
}

type apiEnvelope struct {
	Data struct {
		ID            string          `json:"id"`
		ReservationID string          `json:"reservation_id"`
		Available     int             `json:"available"`
		Confirmed     int             `json:"confirmed"`
		TotalSeats    int             `json:"total_seats"`
		Seats         json.RawMessage `json:"seats"`
	} `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type reserveResult struct {
	status        int
	reservationID string
	apiError      string
	err           error
}

func TestHotSeatStormHasOneWinnerAndMaintainsReconciliation(t *testing.T) {
	app := newIntegrationApp(t)
	stormSize := reservationStormSize(t)
	app.showID = app.createShow(t, "hot-seat-storm", []string{"A1"}, 4)

	stopMonitor := app.monitorReconciliation(app.showID)
	results := app.parallelReserve(stormSize, func(index int) (userID, idempotencyKey string, seats []string) {
		return fmt.Sprintf("hot-buyer-%d", index), fmt.Sprintf("hot-key-%d", index), []string{"A1"}
	})
	if err := stopMonitor(); err != nil {
		t.Fatal(err)
	}

	confirmed, declined, serverErrors := countResults(results)
	t.Logf("hot-seat storm: requests=%d confirmed=%d declined=%d server_errors=%d", stormSize, confirmed, declined, serverErrors)
	if serverErrors != 0 {
		t.Fatalf("expected zero 5xx/network errors, got %d", serverErrors)
	}
	if confirmed != 1 || declined != stormSize-1 {
		t.Fatalf("expected exactly one winner and %d declines, got confirmed=%d declined=%d", stormSize-1, confirmed, declined)
	}

	state := app.getShowForTest(t, app.showID)
	if state.Data.Available+state.Data.Confirmed != state.Data.TotalSeats {
		t.Fatalf("final reconciliation failed: available=%d confirmed=%d total=%d", state.Data.Available, state.Data.Confirmed, state.Data.TotalSeats)
	}
	if state.Data.Available != 0 || state.Data.Confirmed != 1 || state.Data.TotalSeats != 1 {
		t.Fatalf("unexpected final hot-seat state: %+v", state)
	}
	app.assertAvailabilityMetric(t, app.showID, state.Data.Available)
}

func TestConcurrentPerUserLimit(t *testing.T) {
	app := newIntegrationApp(t)
	seatNumbers := make([]string, 10)
	for index := range seatNumbers {
		seatNumbers[index] = fmt.Sprintf("A%d", index+1)
	}
	app.showID = app.createShow(t, "per-user-limit", seatNumbers, 4)

	results := app.parallelReserve(10, func(index int) (userID, idempotencyKey string, seats []string) {
		return "same-buyer", fmt.Sprintf("limit-key-%d", index), []string{seatNumbers[index]}
	})
	confirmed, declined, serverErrors := countResults(results)
	t.Logf("per-user storm: requests=10 confirmed=%d declined=%d server_errors=%d", confirmed, declined, serverErrors)
	if serverErrors != 0 {
		t.Fatalf("expected zero 5xx/network errors, got %d", serverErrors)
	}
	if confirmed != 4 || declined != 6 {
		t.Fatalf("expected four confirmations and six limit declines, got confirmed=%d declined=%d", confirmed, declined)
	}

	state := app.getShowForTest(t, app.showID)
	if state.Data.Available+state.Data.Confirmed != state.Data.TotalSeats {
		t.Fatalf("reconciliation failed: available=%d confirmed=%d total=%d", state.Data.Available, state.Data.Confirmed, state.Data.TotalSeats)
	}
	if state.Data.Confirmed != 4 {
		t.Fatalf("expected four confirmed seats for the user, got %d", state.Data.Confirmed)
	}
}

func TestConcurrentIdempotentRetriesCreateOneReservation(t *testing.T) {
	app := newIntegrationApp(t)
	app.showID = app.createShow(t, "idempotency", []string{"A1", "A2"}, 4)
	const retries = 50

	results := app.parallelReserve(retries, func(int) (userID, idempotencyKey string, seats []string) {
		return "same-buyer", "shared-idempotency-key", []string{"A1"}
	})
	confirmed, declined, serverErrors := countResults(results)
	t.Logf("idempotency storm: requests=%d confirmations=%d declines=%d server_errors=%d", retries, confirmed, declined, serverErrors)
	if serverErrors != 0 || declined != 0 || confirmed != retries {
		for _, result := range results {
			if result.apiError != "" {
				t.Logf("idempotency retry returned %d: %s", result.status, result.apiError)
			}
		}
		existing, lookupErr := db.NewReservationTable(app.database).GetByIdempotencyKey(context.Background(), "shared-idempotency-key")
		t.Logf("stored reservation on replay mismatch: %+v lookup_error=%v expected_show=%s expected_user=same-buyer expected_seats=[A1]", existing, lookupErr, app.showID)
		t.Fatalf("all identical retries should return the original reservation: confirmations=%d declines=%d server_errors=%d", confirmed, declined, serverErrors)
	}

	reservationIDs := make(map[string]struct{}, retries)
	for _, result := range results {
		reservationIDs[result.reservationID] = struct{}{}
	}
	if len(reservationIDs) != 1 {
		t.Fatalf("expected all retries to return one reservation ID, got %d IDs", len(reservationIDs))
	}

	status, _, _, err := app.reserve("same-buyer", "shared-idempotency-key", []string{"A2"})
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusConflict {
		t.Fatalf("expected same key with different seats to return 409, got %d", status)
	}

	state := app.getShowForTest(t, app.showID)
	if state.Data.Available+state.Data.Confirmed != state.Data.TotalSeats {
		t.Fatalf("reconciliation failed: available=%d confirmed=%d total=%d", state.Data.Available, state.Data.Confirmed, state.Data.TotalSeats)
	}
	if state.Data.Available != 1 || state.Data.Confirmed != 1 || state.Data.TotalSeats != 2 {
		t.Fatalf("retries changed more than one seat: %+v", state)
	}
}

func TestConcurrentCancelAndReserveDoNotDeadlock(t *testing.T) {
	app := newIntegrationApp(t)
	app.showID = app.createShow(t, "cancel-reserve-race", []string{"A1"}, 4)
	const userID = "cancel-reserve-buyer"
	reservationID := ""

	for attempt := 0; attempt < 30; attempt++ {
		if reservationID == "" {
			status, createdID, apiError, err := app.reserve(userID, fmt.Sprintf("cancel-race-seed-%d", attempt), []string{"A1"})
			if err != nil || status != http.StatusCreated {
				t.Fatalf("seed reservation failed: status=%d api_error=%s err=%v", status, apiError, err)
			}
			reservationID = createdID
		}

		start := make(chan struct{})
		cancelResult := make(chan reserveResult, 1)
		reserveResultChan := make(chan reserveResult, 1)
		go func(id string) {
			<-start
			status, apiError, err := app.cancel(userID, id)
			cancelResult <- reserveResult{status: status, apiError: apiError, err: err}
		}(reservationID)
		go func(key string) {
			<-start
			status, newID, apiError, err := app.reserve(userID, key, []string{"A1"})
			reserveResultChan <- reserveResult{status: status, reservationID: newID, apiError: apiError, err: err}
		}(fmt.Sprintf("cancel-race-reserve-%d", attempt))
		close(start)

		cancelled := <-cancelResult
		reserved := <-reserveResultChan
		if cancelled.err != nil || cancelled.status != http.StatusOK {
			t.Fatalf("cancel attempt %d failed: status=%d api_error=%s err=%v", attempt, cancelled.status, cancelled.apiError, cancelled.err)
		}
		if reserved.err != nil {
			t.Fatalf("reserve attempt %d transport error: %v", attempt, reserved.err)
		}
		switch reserved.status {
		case http.StatusCreated:
			reservationID = reserved.reservationID
		case http.StatusConflict:
			reservationID = ""
		default:
			t.Fatalf("reserve attempt %d returned unexpected status %d: %s", attempt, reserved.status, reserved.apiError)
		}
	}

	state := app.getShowForTest(t, app.showID)
	if state.Data.Available+state.Data.Confirmed != state.Data.TotalSeats {
		t.Fatalf("cancel/reserve race failed reconciliation: available=%d confirmed=%d total=%d", state.Data.Available, state.Data.Confirmed, state.Data.TotalSeats)
	}
	if state.Data.Available > 1 || state.Data.Confirmed > 1 {
		t.Fatalf("cancel/reserve race produced invalid seat counts: %+v", state.Data)
	}
}

func newIntegrationApp(t *testing.T) *integrationApp {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	adminDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := waitForDatabase(ctx, adminDB); err != nil {
		_ = adminDB.Close()
		t.Fatalf("wait for test database: %v", err)
	}
	if _, err := adminDB.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS pgcrypto"); err != nil {
		_ = adminDB.Close()
		t.Fatalf("ensure pgcrypto is installed: %v", err)
	}

	testSchema := "seatbooking_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := adminDB.ExecContext(ctx, "CREATE SCHEMA \""+testSchema+"\""); err != nil {
		_ = adminDB.Close()
		t.Fatalf("create isolated test schema: %v", err)
	}

	pgxConfig, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		_, _ = adminDB.ExecContext(ctx, "DROP SCHEMA \""+testSchema+"\" CASCADE")
		_ = adminDB.Close()
		t.Fatalf("parse test database URL: %v", err)
	}
	pgxConfig.RuntimeParams["search_path"] = testSchema + ",public"
	migrationDB := stdlib.OpenDB(*pgxConfig)
	migrationDB.SetMaxOpenConns(1)
	if err := applyEmbeddedMigration(ctx, migrationDB); err != nil {
		_ = migrationDB.Close()
		_, _ = adminDB.ExecContext(ctx, "DROP SCHEMA \""+testSchema+"\" CASCADE")
		_ = adminDB.Close()
		t.Fatalf("apply test schema migration: %v", err)
	}
	_ = migrationDB.Close()

	database := stdlib.OpenDB(*pgxConfig)
	database.SetMaxOpenConns(20)
	database.SetMaxIdleConns(10)
	app := &integrationApp{database: database, adminDB: adminDB, testSchema: testSchema}
	t.Cleanup(func() {
		if app.server != nil {
			app.server.Close()
		}
		_ = database.Close()
		_, _ = adminDB.ExecContext(context.Background(), "DROP SCHEMA \""+testSchema+"\" CASCADE")
		_ = adminDB.Close()
	})

	requestMetrics := metrics.New(db.NewShowTable(database))
	loggerContext := logctx.WithLogger(context.Background(), zerolog.New(io.Discard))
	apiRouter := router.New(&router.Router{
		Shows:        service.NewShowService(database),
		Reservations: service.NewReservationService(database),
		Database:     database,
		Metrics:      requestMetrics,
		AppConfig: &config.Config{
			JWTSecret:  integrationJWTSecret,
			JWTExpiry:  time.Hour,
			AdminToken: integrationAdminKey,
		},
		Context: loggerContext,
	})
	app.server = httptest.NewServer(apiRouter)
	transport := &http.Transport{MaxConnsPerHost: 1000, MaxIdleConns: 1000, MaxIdleConnsPerHost: 1000}
	app.client = &http.Client{Transport: transport, Timeout: 120 * time.Second}
	return app
}

func waitForDatabase(ctx context.Context, database *sql.DB) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	var lastErr error
	for {
		if err := database.PingContext(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("database did not become ready: %w (last ping error: %v)", ctx.Err(), lastErr)
		case <-ticker.C:
		}
	}
}

func applyEmbeddedMigration(ctx context.Context, database *sql.DB) error {
	migrationSource, err := iofs.New(migrations.Files, ".")
	if err != nil {
		return fmt.Errorf("open migration source: %w", err)
	}
	databaseDriver, err := postgres.WithInstance(database, &postgres.Config{})
	if err != nil {
		_ = migrationSource.Close()
		return fmt.Errorf("create migration database driver: %w", err)
	}
	migration, err := migrate.NewWithInstance("iofs", migrationSource, "postgres", databaseDriver)
	if err != nil {
		_ = migrationSource.Close()
		_ = databaseDriver.Close()
		return fmt.Errorf("create migration runner: %w", err)
	}
	defer migration.Close()
	if err := migration.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}

func (app *integrationApp) createShow(t *testing.T, name string, seats []string, limit int) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"name": name, "seats": seats, "price_paise": 25000, "per_user_limit": limit,
	})
	if err != nil {
		t.Fatalf("encode show request: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, app.server.URL+router.CreateShowPath, strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("create show request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+integrationAdminKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := app.client.Do(request)
	if err != nil {
		t.Fatalf("create show request failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		responseBody, _ := io.ReadAll(response.Body)
		t.Fatalf("create show returned %d: %s", response.StatusCode, responseBody)
	}
	var envelope apiEnvelope
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode show response: %v", err)
	}
	return envelope.Data.ID
}

func (app *integrationApp) parallelReserve(count int, requestData func(index int) (userID, idempotencyKey string, seats []string)) []reserveResult {
	results := make(chan reserveResult, count)
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(count)
	for index := 0; index < count; index++ {
		go func(index int) {
			defer workers.Done()
			<-start
			userID, idempotencyKey, seats := requestData(index)
			status, reservationID, apiError, err := app.reserve(userID, idempotencyKey, seats)
			results <- reserveResult{status: status, reservationID: reservationID, apiError: apiError, err: err}
		}(index)
	}
	close(start)
	workers.Wait()
	close(results)

	outcomes := make([]reserveResult, 0, count)
	for result := range results {
		outcomes = append(outcomes, result)
	}
	return outcomes
}

func (app *integrationApp) reserve(userID, idempotencyKey string, seats []string) (int, string, string, error) {
	body, err := json.Marshal(map[string]any{"seats": seats, "idempotency_key": idempotencyKey})
	if err != nil {
		return 0, "", "", err
	}
	request, err := http.NewRequest(http.MethodPost, app.server.URL+fmt.Sprintf("/shows/%s/reserve", app.showID), strings.NewReader(string(body)))
	if err != nil {
		return 0, "", "", err
	}
	request.Header.Set("Authorization", "Bearer "+userToken(userID))
	request.Header.Set("Content-Type", "application/json")
	response, err := app.client.Do(request)
	if err != nil {
		return 0, "", "", err
	}
	defer response.Body.Close()

	var envelope apiEnvelope
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return response.StatusCode, "", "", err
	}
	apiError := ""
	if envelope.Error != nil {
		apiError = envelope.Error.Code + ": " + envelope.Error.Message
	}
	return response.StatusCode, envelope.Data.ReservationID, apiError, nil
}

func (app *integrationApp) cancel(userID, reservationID string) (int, string, error) {
	request, err := http.NewRequest(http.MethodPost, app.server.URL+"/reservations/"+reservationID+"/cancel", nil)
	if err != nil {
		return 0, "", err
	}
	request.Header.Set("Authorization", "Bearer "+userToken(userID))
	response, err := app.client.Do(request)
	if err != nil {
		return 0, "", err
	}
	defer response.Body.Close()
	var envelope apiEnvelope
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return response.StatusCode, "", err
	}
	if envelope.Error == nil {
		return response.StatusCode, "", nil
	}
	return response.StatusCode, envelope.Error.Code + ": " + envelope.Error.Message, nil
}

func (app *integrationApp) getShow(showID string) (apiEnvelope, int, error) {
	request, err := http.NewRequest(http.MethodGet, app.server.URL+"/shows/"+showID, nil)
	if err != nil {
		return apiEnvelope{}, 0, err
	}
	response, err := app.client.Do(request)
	if err != nil {
		return apiEnvelope{}, 0, err
	}
	defer response.Body.Close()
	var envelope apiEnvelope
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return apiEnvelope{}, response.StatusCode, err
	}
	return envelope, response.StatusCode, nil
}

func (app *integrationApp) getShowForTest(t *testing.T, showID string) apiEnvelope {
	t.Helper()
	envelope, status, err := app.getShow(showID)
	if err != nil {
		t.Fatalf("get show: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("get show returned %d: %+v", status, envelope.Error)
	}
	return envelope
}

func (app *integrationApp) assertReconciled(showID string) error {
	envelope, status, err := app.getShow(showID)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("get show returned %d: %+v", status, envelope.Error)
	}
	if envelope.Data.Available+envelope.Data.Confirmed != envelope.Data.TotalSeats {
		return fmt.Errorf("show %s failed reconciliation: available=%d confirmed=%d total=%d", showID, envelope.Data.Available, envelope.Data.Confirmed, envelope.Data.TotalSeats)
	}
	return nil
}

func (app *integrationApp) monitorReconciliation(showID string) func() error {
	if err := app.assertReconciled(showID); err != nil {
		return func() error { return err }
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			if err := app.assertReconciled(showID); err != nil {
				result <- err
				return
			}
			select {
			case <-ctx.Done():
				result <- nil
				return
			case <-ticker.C:
			}
		}
	}()
	return func() error {
		cancel()
		return <-result
	}
}

func (app *integrationApp) assertAvailabilityMetric(t *testing.T, showID string, available int) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, app.server.URL+router.MetricsPath, nil)
	if err != nil {
		t.Fatalf("create metrics request: %v", err)
	}
	response, err := app.client.Do(request)
	if err != nil {
		t.Fatalf("fetch metrics: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read metrics response: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("metrics endpoint returned %d: %s", response.StatusCode, body)
	}
	expected := fmt.Sprintf(`seatbooking_seats_available{show_id="%s"} %d`, showID, available)
	if !strings.Contains(string(body), expected) {
		t.Fatalf("metrics did not contain %q", expected)
	}
}

func countResults(results []reserveResult) (confirmed, declined, serverErrors int) {
	for _, result := range results {
		if result.err != nil {
			serverErrors++
			continue
		}
		switch result.status {
		case http.StatusCreated:
			confirmed++
		case http.StatusConflict:
			declined++
		default:
			if result.status >= http.StatusInternalServerError {
				serverErrors++
			} else {
				declined++
			}
		}
	}
	return confirmed, declined, serverErrors
}

func reservationStormSize(t *testing.T) int {
	t.Helper()
	value := os.Getenv("SEATBOOKING_STORM_SIZE")
	if value == "" {
		return 500
	}
	count, err := strconv.Atoi(value)
	if err != nil || count < 1 {
		t.Fatalf("SEATBOOKING_STORM_SIZE must be a positive integer, got %q", value)
	}
	return count
}

func userToken(userID string) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   userID,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	})
	signed, err := token.SignedString([]byte(integrationJWTSecret))
	if err != nil {
		panic(err)
	}
	return signed
}
