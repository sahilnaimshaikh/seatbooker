package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/paytm-hack/seatbooking/pkg/service"
)

type closeTrackingBody struct {
	*strings.Reader
	closed bool
}

func (body *closeTrackingBody) Close() error {
	body.closed = true
	return nil
}

func TestWriteServiceErrorMapsReservationOutcomes(t *testing.T) {
	tests := []struct {
		code       service.Code
		wantStatus int
	}{
		{code: service.CodeNotFound, wantStatus: http.StatusNotFound},
		{code: service.CodeInvalidInput, wantStatus: http.StatusBadRequest},
		{code: service.CodeConflict, wantStatus: http.StatusConflict},
		{code: service.CodeSeatTaken, wantStatus: http.StatusConflict},
		{code: service.CodePerUserLimit, wantStatus: http.StatusConflict},
		{code: service.CodeIdempotencyConflict, wantStatus: http.StatusConflict},
	}

	for _, test := range tests {
		t.Run(string(test.code), func(t *testing.T) {
			response := httptest.NewRecorder()
			writeServiceError(response, &service.Error{Code: test.code, Message: "test"})
			if response.Code != test.wantStatus {
				t.Fatalf("expected status %d, got %d", test.wantStatus, response.Code)
			}
		})
	}
}

func TestDecodeJSONClosesRequestBody(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	body := &closeTrackingBody{Reader: strings.NewReader(`{"user_id":"buyer"}`)}
	request.Body = body
	response := httptest.NewRecorder()
	var input LoginRequest

	if err := decodeJSON(response, request, &input); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if !body.closed {
		t.Fatal("expected request body to be closed after decoding")
	}
}
