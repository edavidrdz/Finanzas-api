package mailer

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

type SMTPConfig struct {
	Host     string
	Port     string
	Username string
	Password string
	From     string
}

func (c SMTPConfig) Configured() bool {
	return c.Host != "" && c.Port != "" && c.From != ""
}

func sanitizeHeaderValue(value string) string {
	value = strings.ReplaceAll(value, "\r", "")
	value = strings.ReplaceAll(value, "\n", "")
	return value
}

func normalizeBody(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.ReplaceAll(value, "\n", "\r\n")
}

func (c SMTPConfig) Send(ctx context.Context, to, subject, body string) error {
	if !c.Configured() {
		return errors.New("SMTP no está configurado")
	}
	if (c.Username == "") != (c.Password == "") {
		return errors.New("SMTP_USERNAME y SMTP_PASSWORD deben configurarse juntos")
	}
	port, err := strconv.Atoi(c.Port)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("SMTP_PORT inválido")
	}
	fromAddress, err := mail.ParseAddress(c.From)
	if err != nil {
		return fmt.Errorf("SMTP_FROM inválido: %w", err)
	}
	toAddress, err := mail.ParseAddress(to)
	if err != nil {
		return fmt.Errorf("destinatario inválido: %w", err)
	}

	hostPort := net.JoinHostPort(c.Host, c.Port)
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	if port == 465 {
		conn, err = tls.DialWithDialer(dialer, "tcp", hostPort, &tls.Config{
			ServerName: c.Host,
			MinVersion: tls.VersionTLS12,
		})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", hostPort)
	}
	if err != nil {
		return fmt.Errorf("conectar con SMTP: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))

	client, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		return fmt.Errorf("iniciar sesión SMTP: %w", err)
	}
	defer client.Close()

	if port != 465 {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("el servidor SMTP no ofrece STARTTLS")
		}
		if err := client.StartTLS(&tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("iniciar TLS SMTP: %w", err)
		}
	}
	if c.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", c.Username, c.Password, c.Host)); err != nil {
			return fmt.Errorf("autenticar SMTP: %w", err)
		}
	}
	if err := client.Mail(fromAddress.Address); err != nil {
		return fmt.Errorf("definir remitente SMTP: %w", err)
	}
	if err := client.Rcpt(toAddress.Address); err != nil {
		return fmt.Errorf("definir destinatario SMTP: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("iniciar mensaje SMTP: %w", err)
	}
	message := strings.Join([]string{
		"From: " + fromAddress.String(),
		"To: " + toAddress.String(),
		"Subject: " + mime.QEncoding.Encode("UTF-8", sanitizeHeaderValue(subject)),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		normalizeBody(body),
	}, "\r\n")
	if _, err := fmt.Fprint(writer, message); err != nil {
		_ = writer.Close()
		return fmt.Errorf("escribir mensaje SMTP: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("enviar mensaje SMTP: %w", err)
	}
	return client.Quit()
}
