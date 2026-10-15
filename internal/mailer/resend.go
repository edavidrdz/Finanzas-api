package mailer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const resendAPIURL = "https://api.resend.com/emails"

type ResendConfig struct {
	APIKey string
	From   string
}

type resendEmail struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
}

func (c ResendConfig) Configured() bool {
	return c.APIKey != "" && c.From != ""
}

func (c ResendConfig) Send(ctx context.Context, to, subject, body string) error {
	return c.sendWithClient(ctx, to, subject, body, &http.Client{Timeout: 15 * time.Second})
}

func (c ResendConfig) sendWithClient(ctx context.Context, to, subject, body string, client *http.Client) error {
	if !c.Configured() {
		return errors.New("Resend no está configurado")
	}

	payload, err := json.Marshal(resendEmail{
		From: c.From, To: []string{to}, Subject: subject, Text: body,
	})
	if err != nil {
		return fmt.Errorf("preparar correo para Resend: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resendAPIURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("crear solicitud a Resend: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("solicitar envío a Resend: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		responseBody, readErr := io.ReadAll(io.LimitReader(res.Body, 4096))
		if readErr != nil {
			return fmt.Errorf("Resend devolvió HTTP %d y no se pudo leer el detalle: %w", res.StatusCode, readErr)
		}
		detail := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(string(responseBody), "\r", " "), "\n", " "))
		if detail == "" {
			return fmt.Errorf("Resend devolvió HTTP %d", res.StatusCode)
		}
		return fmt.Errorf("Resend devolvió HTTP %d: %s", res.StatusCode, detail)
	}
	return nil
}
