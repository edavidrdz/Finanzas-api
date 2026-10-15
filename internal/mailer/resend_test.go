package mailer

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestResendSend(t *testing.T) {
	config := ResendConfig{APIKey: "re_test_key", From: "Mis Finanzas <no-reply@example.com>"}
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.String() != resendAPIURL {
			t.Errorf("unexpected request: %s %s", req.Method, req.URL)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer re_test_key" {
			t.Errorf("Authorization = %q", got)
		}
		var payload resendEmail
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if payload.From != config.From || len(payload.To) != 1 || payload.To[0] != "user@example.com" ||
			payload.Subject != "Restablecer contraseña" || payload.Text != "Abre el enlace." {
			t.Errorf("unexpected email payload: %+v", payload)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":"email-id"}`)), Header: make(http.Header)}, nil
	})}
	if err := config.sendWithClient(context.Background(), "user@example.com", "Restablecer contraseña", "Abre el enlace.", client); err != nil {
		t.Fatalf("sendWithClient() error = %v", err)
	}
}

func TestResendSendReportsAPIError(t *testing.T) {
	config := ResendConfig{APIKey: "re_test_key", From: "no-reply@example.com"}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Body:       io.NopCloser(strings.NewReader(`{"message":"invalid api key"}`)),
			Header:     make(http.Header),
		}, nil
	})}
	err := config.sendWithClient(context.Background(), "user@example.com", "subject", "body", client)
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") || !strings.Contains(err.Error(), "invalid api key") {
		t.Fatalf("sendWithClient() error = %v, want Resend HTTP error", err)
	}
}

func TestResendSendRequiresConfiguration(t *testing.T) {
	err := (ResendConfig{}).Send(context.Background(), "user@example.com", "subject", "body")
	if err == nil {
		t.Fatal("Send() should reject missing Resend configuration")
	}
}
