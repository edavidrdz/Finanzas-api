package handlers

import (
	"testing"
	"time"
)

func TestSiguienteFechaProgramada(t *testing.T) {
	tests := []struct {
		name       string
		actual     string
		frecuencia string
		want       string
		dia        int
		wantErr    bool
	}{
		{name: "monthly clamps month end", actual: "2026-01-31", frecuencia: "mensual", dia: 31, want: "2026-02-28"},
		{name: "month end anchor is restored next month", actual: "2026-02-28", frecuencia: "mensual", dia: 31, want: "2026-03-31"},
		{name: "quarterly preserves day", actual: "2026-01-15", frecuencia: "trimestral", dia: 15, want: "2026-04-15"},
		{name: "semiannual clamps month end", actual: "2024-08-31", frecuencia: "semestral", dia: 31, want: "2025-02-28"},
		{name: "annual leap day clamps", actual: "2024-02-29", frecuencia: "anual", dia: 29, want: "2025-02-28"},
		{name: "annual leap day is restored", actual: "2027-02-28", frecuencia: "anual", dia: 29, want: "2028-02-29"},
		{name: "one time is not advanced", actual: "2026-01-01", frecuencia: "unico", dia: 1, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := time.Parse(dateLayout, test.actual)
			if err != nil {
				t.Fatal(err)
			}
			got, err := siguienteFechaProgramada(actual, test.frecuencia, test.dia)
			if (err != nil) != test.wantErr {
				t.Fatalf("siguienteFechaProgramada() error = %v, wantErr %v", err, test.wantErr)
			}
			if !test.wantErr && got.Format(dateLayout) != test.want {
				t.Fatalf("siguienteFechaProgramada() = %s, want %s", got.Format(dateLayout), test.want)
			}
		})
	}
}

func TestAportacionSugerida(t *testing.T) {
	hoySemestral := time.Date(2026, time.October, 14, 0, 0, 0, 0, time.UTC)
	sugerida, dias := aportacionSugerida(12000, 0, "2027-04-14", hoySemestral)
	if sugerida != 2000 || dias != 182 {
		t.Fatalf("aportacionSugerida() semestral = (%v, %d), want (2000, 182)", sugerida, dias)
	}

	hoy := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	sugerida, dias = aportacionSugerida(1200, 300, "2026-04-01", hoy)
	if sugerida != 300 || dias != 90 {
		t.Fatalf("aportacionSugerida() = (%v, %d), want (300, 90)", sugerida, dias)
	}

	sugerida, dias = aportacionSugerida(800, 1000, "2026-01-01", hoy)
	if sugerida != 0 || dias != 0 {
		t.Fatalf("aportacionSugerida() for funded amount = (%v, %d), want (0, 0)", sugerida, dias)
	}
}

func TestSiguienteFechaIngreso(t *testing.T) {
	actual := time.Date(2026, time.January, 31, 0, 0, 0, 0, time.UTC)
	got, err := siguienteFechaIngreso(actual, "semanal", 31)
	if err != nil || got.Format(dateLayout) != "2026-02-07" {
		t.Fatalf("siguienteFechaIngreso() semanal = (%s, %v), want 2026-02-07", got.Format(dateLayout), err)
	}

	got, err = siguienteFechaIngreso(actual, "quincenal", 31)
	if err != nil || got.Format(dateLayout) != "2026-02-14" {
		t.Fatalf("siguienteFechaIngreso() quincenal = (%s, %v), want 2026-02-14", got.Format(dateLayout), err)
	}
}

func TestCalcularDisponibleQuincenaNoDuplicaCargosDeTarjeta(t *testing.T) {
	got := calcularDisponibleQuincena(500, 10000, 200, -50, 3000, 2500)
	if got != 5150 {
		t.Fatalf("calcularDisponibleQuincena() = %v, want 5150", got)
	}
}
