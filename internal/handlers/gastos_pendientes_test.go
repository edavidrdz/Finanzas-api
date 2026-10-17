package handlers

import (
	"strings"
	"testing"
)

func TestValidarUbicacion(t *testing.T) {
	lat, lon := 19.4326, -99.1332
	name := "  Bodega Aurrera  "
	tests := []struct {
		name    string
		lat     *float64
		lon     *float64
		namePtr *string
		want    string
	}{
		{name: "no location", want: ""},
		{name: "valid coordinates", lat: &lat, lon: &lon, namePtr: &name, want: ""},
		{name: "latitude without longitude", lat: &lat, want: "deben enviarse juntas"},
		{name: "out of range", lat: floatPointer(90.1), lon: &lon, want: "inválidas"},
		{name: "name too long", namePtr: stringPointer(strings.Repeat("x", 201)), want: "200 caracteres"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validarUbicacion(test.lat, test.lon, test.namePtr); !strings.Contains(got, test.want) {
				t.Fatalf("validarUbicacion() = %q, want substring %q", got, test.want)
			}
		})
	}
}

func TestValidarGastoPendiente(t *testing.T) {
	tests := []struct {
		name  string
		input gastoPendienteInput
		want  string
	}{
		{
			name: "empty draft is valid",
			input: gastoPendienteInput{
				Fecha: "2026-10-17", IniciadaEn: "2026-10-17T16:24:15.583Z",
			},
		},
		{
			name: "positive partial amount",
			input: gastoPendienteInput{
				Monto: floatPointer(85.5), Fecha: "2026-10-17", IniciadaEn: "2026-10-17T16:24:15Z",
			},
		},
		{
			name: "rejects zero amount",
			input: gastoPendienteInput{
				Monto: floatPointer(0), Fecha: "2026-10-17", IniciadaEn: "2026-10-17T16:24:15Z",
			},
			want: "mayor a 0",
		},
		{
			name: "rejects invalid date",
			input: gastoPendienteInput{
				Fecha: "2026-02-30", IniciadaEn: "2026-10-17T16:24:15Z",
			},
			want: "fecha",
		},
		{
			name:  "requires RFC3339 timestamp",
			input: gastoPendienteInput{Fecha: "2026-10-17", IniciadaEn: "not-a-date"},
			want:  "RFC3339",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validarGastoPendiente(test.input); !strings.Contains(got, test.want) {
				t.Fatalf("validarGastoPendiente() = %q, want substring %q", got, test.want)
			}
		})
	}
}

func floatPointer(value float64) *float64 { return &value }
func stringPointer(value string) *string  { return &value }
