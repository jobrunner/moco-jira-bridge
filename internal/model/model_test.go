package model

import (
	"testing"
	"time"
)

func TestDayNormalisiertAufMitternachtLokal(t *testing.T) {
	in := time.Date(2026, 9, 18, 23, 45, 12, 500, time.Local)
	got := Day(in)
	want := time.Date(2026, 9, 18, 0, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("Day() = %v, want %v", got, want)
	}
}

func TestDayKonvertiertNachLokalVorDemAbschneiden(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("Zeitzonendaten nicht verfügbar")
	}
	in := time.Date(2026, 9, 18, 1, 30, 0, 0, time.UTC)
	got := Day(in.In(berlin))
	if got.Day() != 18 || got.Month() != time.September {
		t.Fatalf("Day() = %v, want 2026-09-18", got)
	}
}

func TestRoundMinutes(t *testing.T) {
	cases := []struct {
		name string
		in   time.Duration
		want time.Duration
	}{
		{"exakt", 90 * time.Minute, 90 * time.Minute},
		{"abrunden", 90*time.Minute + 20*time.Second, 90 * time.Minute},
		{"aufrunden", 90*time.Minute + 40*time.Second, 91 * time.Minute},
		{"float-artefakt 0.1h", time.Duration(0.1*float64(time.Hour)) + 3, 6 * time.Minute},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RoundMinutes(c.in); got != c.want {
				t.Fatalf("RoundMinutes(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestDiffDelta(t *testing.T) {
	d := Diff{Want: 120 * time.Minute, Have: 90 * time.Minute}
	if got := d.Delta(); got != 30*time.Minute {
		t.Fatalf("Delta() = %v, want 30m", got)
	}
	d = Diff{Want: 60 * time.Minute, Have: 90 * time.Minute}
	if got := d.Delta(); got != -30*time.Minute {
		t.Fatalf("Delta() = %v, want -30m", got)
	}
}
