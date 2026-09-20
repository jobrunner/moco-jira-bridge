package timerange

import (
	"testing"
	"time"
)

var today = time.Date(2026, 9, 18, 14, 30, 0, 0, time.Local)

func check(t *testing.T, r Range, from, to string) {
	t.Helper()
	if r.From.Format("2006-01-02") != from || r.To.Format("2006-01-02") != to {
		t.Fatalf("Range = %s bis %s, want %s bis %s",
			r.From.Format("2006-01-02"), r.To.Format("2006-01-02"), from, to)
	}
}

func TestResolveDefaultIstLaufenderMonatBisHeute(t *testing.T) {
	r, err := Resolve("", "", "", "", today)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	check(t, r, "2026-09-01", "2026-09-18")
}

func TestResolveMonatIstGanzerMonat(t *testing.T) {
	r, err := Resolve("2026-08", "", "", "", today)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	check(t, r, "2026-08-01", "2026-08-31")
}

func TestResolveMonatFebruarSchaltjahr(t *testing.T) {
	r, err := Resolve("2028-02", "", "", "", today)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	check(t, r, "2028-02-01", "2028-02-29")
}

func TestResolveUntilIstLaufenderMonatBisStichtag(t *testing.T) {
	r, err := Resolve("", "", "", "2026-09-10", today)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	check(t, r, "2026-09-01", "2026-09-10")
}

func TestResolveFromTo(t *testing.T) {
	r, err := Resolve("", "2026-09-05", "2026-09-12", "", today)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	check(t, r, "2026-09-05", "2026-09-12")
}

func TestResolveFehlerBeiKombination(t *testing.T) {
	if _, err := Resolve("2026-09", "2026-09-05", "", "", today); err == nil {
		t.Fatal("--month und --from zusammen sollten einen Fehler geben")
	}
}

func TestResolveFehlerWennNurFromGesetzt(t *testing.T) {
	if _, err := Resolve("", "2026-09-05", "", "", today); err == nil {
		t.Fatal("--from ohne --to sollte einen Fehler geben")
	}
}

func TestResolveFehlerBeiVerdrehtemZeitraum(t *testing.T) {
	if _, err := Resolve("", "2026-09-12", "2026-09-05", "", today); err == nil {
		t.Fatal("To vor From sollte einen Fehler geben")
	}
}

func TestResolveFehlerBeiUngueltigemDatum(t *testing.T) {
	if _, err := Resolve("2026-13", "", "", "", today); err == nil {
		t.Fatal("ungültiger Monat sollte einen Fehler geben")
	}
}

func TestStringFormat(t *testing.T) {
	r, _ := Resolve("2026-09", "", "", "", today)
	if got := r.String(); got != "2026-09-01 bis 2026-09-30" {
		t.Fatalf("String() = %q", got)
	}
}
