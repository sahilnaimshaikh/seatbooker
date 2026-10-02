package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const defaultStormSize = 20000
const defaultConnections = 1000

var declineReasons = []string{
	"seat-taken",
	"per-user-limit",
	"idempotent-replay",
	"lock-timeout",
}

type burstClient struct {
	baseURL    string
	adminToken string
	jwtSecret  string
	http       *http.Client
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "burst:", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) != 1 {
		return errors.New("usage: ./burst.sh <BASE_URL>")
	}

	parsedURL, err := url.Parse(strings.TrimRight(arguments[0], "/"))
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return errors.New("BASE_URL must be an http or https URL")
	}
	if !isLocalHost(parsedURL.Hostname()) && os.Getenv("ALLOW_REMOTE_BURST") != "true" {
		return errors.New("remote targets create permanent test shows; set ALLOW_REMOTE_BURST=true to continue")
	}

	adminToken := os.Getenv("ADMIN_TOKEN")
	jwtSecret := os.Getenv("JWT_SECRET")
	if adminToken == "" || jwtSecret == "" {
		return errors.New("ADMIN_TOKEN and JWT_SECRET environment variables are required")
	}

	stormSize, err := positiveIntEnv("BURST_SIZE", defaultStormSize)
	if err != nil {
		return err
	}
	connections, err := positiveIntEnv("BURST_CONNECTIONS", defaultConnections)
	if err != nil {
		return err
	}

	transport := &http.Transport{
		MaxConnsPerHost:     connections,
		MaxIdleConns:        connections,
		MaxIdleConnsPerHost: connections,
	}
	defer transport.CloseIdleConnections()
	client := &burstClient{
		baseURL:    strings.TrimRight(arguments[0], "/"),
		adminToken: adminToken,
		jwtSecret:  jwtSecret,
		http:       &http.Client{Transport: transport, Timeout: 5 * time.Minute},
	}

	runID := uuid.NewString()
	fmt.Printf("Target: %s\n", client.baseURL)
	fmt.Printf("Hot-seat requests: %d (max concurrent connections: %d)\n", stormSize, connections)
	fmt.Println("This run creates three shows and leaves them in the target database.")

	if err := runHotSeat(client, runID, stormSize); err != nil {
		return err
	}
	if err := runPerUserLimit(client, runID); err != nil {
		return err
	}
	if err := runIdempotency(client, runID); err != nil {
		return err
	}
	fmt.Println("Burst checks passed.")
	return nil
}

func runHotSeat(client *burstClient, runID string, stormSize int) error {
	showID, err := client.createShow("burst-hot-"+runID, []string{"A1"}, 4)
	if err != nil {
		return fmt.Errorf("create hot-seat show: %w", err)
	}
	results := reserveConcurrently(stormSize, func(index int) reservationAttempt {
		return client.reserve(fmt.Sprintf("hot-buyer-%d-%s", index, runID), fmt.Sprintf("hot-key-%d-%s", index, runID), showID, []string{"A1"})
	})
	outcomes := summarize(results)
	state, metricAvailable, err := client.reconcile(showID)
	if err != nil {
		return fmt.Errorf("reconcile hot-seat show: %w", err)
	}
	printScenario("hot-seat", outcomes, state, metricAvailable)

	if err := checkNoServerErrors("hot-seat", outcomes); err != nil {
		return err
	}
	if outcomes.newConfirmations != 1 || outcomes.declinedByReason["seat-taken"] != stormSize-1 {
		return fmt.Errorf("hot-seat expected one 201 and %d seat-taken 409s", stormSize-1)
	}
	if state.available != 0 || state.confirmed != 1 || state.total != 1 {
		return errors.New("hot-seat final API state was unexpected")
	}
	if metricAvailable != state.available {
		return errors.New("hot-seat availability gauge does not match API state")
	}
	return nil
}

func runPerUserLimit(client *burstClient, runID string) error {
	seats := make([]string, 10)
	for index := range seats {
		seats[index] = fmt.Sprintf("A%d", index+1)
	}
	showID, err := client.createShow("burst-limit-"+runID, seats, 4)
	if err != nil {
		return fmt.Errorf("create per-user-limit show: %w", err)
	}
	results := reserveConcurrently(len(seats), func(index int) reservationAttempt {
		return client.reserve("limit-buyer-"+runID, fmt.Sprintf("limit-key-%d-%s", index, runID), showID, []string{seats[index]})
	})
	outcomes := summarize(results)
	state, metricAvailable, err := client.reconcile(showID)
	if err != nil {
		return fmt.Errorf("reconcile per-user-limit show: %w", err)
	}
	printScenario("per-user-limit", outcomes, state, metricAvailable)

	if err := checkNoServerErrors("per-user-limit", outcomes); err != nil {
		return err
	}
	if outcomes.newConfirmations != 4 || outcomes.declinedByReason["per-user-limit"] != 6 {
		return errors.New("per-user-limit expected four 201s and six per-user-limit 409s")
	}
	if state.available+state.confirmed != state.total || state.confirmed != 4 {
		return errors.New("per-user-limit final state did not reconcile to four confirmed seats")
	}
	if metricAvailable != state.available {
		return errors.New("per-user-limit availability gauge does not match API state")
	}
	return nil
}

func runIdempotency(client *burstClient, runID string) error {
	showID, err := client.createShow("burst-idempotency-"+runID, []string{"A1", "A2"}, 4)
	if err != nil {
		return fmt.Errorf("create idempotency show: %w", err)
	}
	const retries = 50
	results := reserveConcurrently(retries, func(int) reservationAttempt {
		return client.reserve("idempotent-buyer-"+runID, "shared-key-"+runID, showID, []string{"A1"})
	})

	reservationIDs := make(map[string]struct{}, retries)
	for _, result := range results {
		if result.status == http.StatusCreated && result.reservationID != "" {
			reservationIDs[result.reservationID] = struct{}{}
		}
	}
	if len(reservationIDs) != 1 {
		return fmt.Errorf("identical retries returned %d unique reservation IDs, expected one", len(reservationIDs))
	}

	mismatchedBody := client.reserve("idempotent-buyer-"+runID, "shared-key-"+runID, showID, []string{"A2"})
	results = append(results, mismatchedBody)
	outcomes := summarize(results)
	state, metricAvailable, err := client.reconcile(showID)
	if err != nil {
		return fmt.Errorf("reconcile idempotency show: %w", err)
	}
	printScenario("idempotency", outcomes, state, metricAvailable)

	if err := checkNoServerErrors("idempotency", outcomes); err != nil {
		return err
	}
	if outcomes.newConfirmations != 1 || outcomes.idempotentReplays != retries-1 || outcomes.declinedByReason["idempotent-replay"] != 1 {
		return fmt.Errorf("idempotency expected one new confirmation, %d replays, and one same-key/different-body decline", retries-1)
	}
	if state.available+state.confirmed != state.total || state.available != 1 || state.confirmed != 1 {
		return errors.New("idempotency retries changed more than one seat or failed reconciliation")
	}
	if metricAvailable != state.available {
		return errors.New("idempotency availability gauge does not match API state")
	}
	return nil
}

func (client *burstClient) createShow(name string, seats []string, perUserLimit int) (string, error) {
	body := createShowRequest{Name: name, Seats: seats, PricePaise: 25000, PerUserLimit: perUserLimit}
	status, envelope, err := client.postJSON("/shows", client.adminToken, body)
	if err != nil {
		return "", err
	}
	if status != http.StatusCreated {
		return "", fmt.Errorf("POST /shows returned %d: %s", status, errorText(envelope.Error))
	}
	return envelope.Data.ID, nil
}

func (client *burstClient) reserve(userID, idempotencyKey, showID string, seats []string) reservationAttempt {
	token, err := client.userToken(userID)
	if err != nil {
		return reservationAttempt{err: err}
	}
	status, envelope, err := client.postJSON(fmt.Sprintf("/shows/%s/reserve", showID), token, reserveRequest{
		Seats: seats, IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return reservationAttempt{status: status, err: err}
	}
	return reservationAttempt{
		status:        status,
		reservationID: envelope.Data.ReservationID,
		apiError:      envelope.Error,
	}
}

func (client *burstClient) postJSON(path, bearerToken string, payload any) (int, apiEnvelope, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, apiEnvelope{}, err
	}
	request, err := http.NewRequest(http.MethodPost, client.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return 0, apiEnvelope{}, err
	}
	request.Header.Set("Authorization", "Bearer "+bearerToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return 0, apiEnvelope{}, err
	}
	defer response.Body.Close()
	var envelope apiEnvelope
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return response.StatusCode, apiEnvelope{}, err
	}
	return response.StatusCode, envelope, nil
}

func (client *burstClient) userToken(userID string) (string, error) {
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   userID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	})
	return token.SignedString([]byte(client.jwtSecret))
}

func (client *burstClient) reconcile(showID string) (showState, int, error) {
	request, err := http.NewRequest(http.MethodGet, client.baseURL+fmt.Sprintf("/shows/%s", showID), nil)
	if err != nil {
		return showState{}, 0, err
	}
	response, err := client.http.Do(request)
	if err != nil {
		return showState{}, 0, err
	}
	defer response.Body.Close()
	var envelope apiEnvelope
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return showState{}, 0, err
	}
	if response.StatusCode != http.StatusOK {
		return showState{}, 0, fmt.Errorf("GET /shows/%s returned %d: %s", showID, response.StatusCode, errorText(envelope.Error))
	}

	metricAvailable, err := client.availableSeatMetric(showID)
	if err != nil {
		return showState{}, 0, err
	}
	return showState{available: envelope.Data.Available, confirmed: envelope.Data.Confirmed, total: envelope.Data.TotalSeats}, metricAvailable, nil
}

func (client *burstClient) availableSeatMetric(showID string) (int, error) {
	response, err := client.http.Get(client.baseURL + "/metrics")
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("GET /metrics returned %d", response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return 0, err
	}
	prefix := fmt.Sprintf(`seatbooking_seats_available{show_id="%s"} `, showID)
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, prefix) {
			return strconv.Atoi(strings.TrimPrefix(line, prefix))
		}
	}
	return 0, fmt.Errorf("availability metric for show %s was not found", showID)
}

func reserveConcurrently(count int, buildRequest func(index int) reservationAttempt) []reservationAttempt {
	results := make(chan reservationAttempt, count)
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(count)
	for index := 0; index < count; index++ {
		go func(index int) {
			defer workers.Done()
			<-start
			results <- buildRequest(index)
		}(index)
	}
	close(start)
	workers.Wait()
	close(results)

	attempts := make([]reservationAttempt, 0, count)
	for result := range results {
		attempts = append(attempts, result)
	}
	return attempts
}

func summarize(attempts []reservationAttempt) scenarioResults {
	results := scenarioResults{requests: len(attempts), declinedByReason: make(map[string]int)}
	reservationIDs := make(map[string]struct{})
	for _, attempt := range attempts {
		if attempt.status >= http.StatusInternalServerError {
			results.http5xx++
			continue
		}
		if attempt.err != nil {
			results.transportErrors++
			continue
		}
		if attempt.status == http.StatusCreated {
			if _, exists := reservationIDs[attempt.reservationID]; exists {
				results.idempotentReplays++
			} else {
				reservationIDs[attempt.reservationID] = struct{}{}
				results.newConfirmations++
			}
			continue
		}

		reason := "http-" + strconv.Itoa(attempt.status)
		if attempt.apiError != nil {
			switch attempt.apiError.Code {
			case "seat_taken":
				reason = "seat-taken"
			case "per_user_limit":
				reason = "per-user-limit"
			case "idempotency_key_reused":
				reason = "idempotent-replay"
			case "conflict":
				reason = "lock-timeout"
			}
		}
		results.declinedByReason[reason]++
	}
	return results
}

func printScenario(name string, results scenarioResults, state showState, metricAvailable int) {
	fmt.Printf("%s: requests=%d new_confirmed=%d idempotent_replays=%d http_5xx=%d transport_errors=%d\n",
		name, results.requests, results.newConfirmations, results.idempotentReplays, results.http5xx, results.transportErrors)
	for _, reason := range declineReasons {
		fmt.Printf("  declined[%s]=%d\n", reason, results.declinedByReason[reason])
	}
	fmt.Printf("  final_state: available=%d confirmed=%d total=%d metrics_available=%d\n",
		state.available, state.confirmed, state.total, metricAvailable)
}

func checkNoServerErrors(name string, results scenarioResults) error {
	if results.http5xx != 0 || results.transportErrors != 0 {
		return fmt.Errorf("%s had %d HTTP 5xx and %d transport errors", name, results.http5xx, results.transportErrors)
	}
	return nil
}

func isLocalHost(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

func positiveIntEnv(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return parsed, nil
}

func errorText(apiErr *apiError) string {
	if apiErr == nil {
		return "empty error response"
	}
	return apiErr.Code + ": " + apiErr.Message
}
