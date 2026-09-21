// Command mjb gleicht Moco-Zeitbuchungen mit Jira/Tempo ab.
// Moco ist immer das führende System.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"moco-jira-bridge/internal/app"
	"moco-jira-bridge/internal/config"
	doctorpkg "moco-jira-bridge/internal/doctor"
	"moco-jira-bridge/internal/jira"
	"moco-jira-bridge/internal/moco"
	"moco-jira-bridge/internal/parse"
	"moco-jira-bridge/internal/report"
	syncpkg "moco-jira-bridge/internal/sync"
	"moco-jira-bridge/internal/timerange"
)

const usage = `mjb — Moco-Zeiten gegen Jira/Tempo abgleichen

Kommandos:
  validate    Prüft, ob Moco und Jira übereinstimmen (schreibt nie)
  sync        Zieht fehlende Zeiten aus Moco in Jira nach
  doctor      Prüft Konfiguration und Erreichbarkeit beider Systeme

Zeitraum (für validate und sync):
  --month JJJJ-MM        ganzer Monat
  --from JJJJ-MM-TT      Beginn (zusammen mit --to)
  --to JJJJ-MM-TT        Ende
  --until JJJJ-MM-TT     laufender Monat bis zu diesem Stichtag
  ohne Angabe            laufender Monat bis heute

Weitere Flags:
  --apply                nur bei sync: tatsächlich schreiben (Default: Dry-Run)
  --json                 maschinenlesbare Ausgabe
  --config PFAD          Konfigurationsdatei (Default: ~/.config/moco-jira-bridge/config.yaml)

Tokens kommen aus der Umgebung: MOCO_API_KEY, JIRA_API_TOKEN

Exit-Codes: 0 sauber, 1 Abweichungen oder Hinweise, 2 Konfigurations-/Verbindungsfehler
`

func main() {
	os.Exit(run())
}

func run() int {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}

	command := os.Args[1]
	switch command {
	case "validate", "sync", "doctor":
	case "-h", "--help", "help":
		fmt.Print(usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unbekanntes Kommando %q\n\n%s", command, usage)
		return 2
	}

	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		month      = fs.String("month", "", "Monat JJJJ-MM")
		from       = fs.String("from", "", "Beginn JJJJ-MM-TT")
		to         = fs.String("to", "", "Ende JJJJ-MM-TT")
		until      = fs.String("until", "", "Stichtag JJJJ-MM-TT im laufenden Monat")
		apply      = fs.Bool("apply", false, "tatsächlich nach Jira schreiben")
		asJSON     = fs.Bool("json", false, "maschinenlesbare Ausgabe")
		configPath = fs.String("config", config.DefaultPath(), "Pfad zur Konfiguration")
	)
	if err := fs.Parse(os.Args[2:]); err != nil {
		return 2
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fail(err)
	}

	parser, err := parse.NewParser(cfg.Ticket.Pattern, cfg.Ticket.MarkerPattern)
	if err != nil {
		return fail(err)
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	ctx := context.Background()

	if command == "doctor" {
		return doctor(ctx, cfg, httpClient)
	}

	target, err := jira.NewTarget(cfg.Jira, httpClient)
	if err != nil {
		return fail(err)
	}

	hour, minute, loc, err := cfg.StartClock()
	if err != nil {
		return fail(err)
	}

	application := &app.App{
		Moco:            moco.New(cfg.Moco, parser, httpClient),
		Jira:            target,
		ExpectedMarkers: cfg.ExpectedMarkers(),
		Clock:           syncpkg.Clock{Hour: hour, Minute: minute, Loc: loc},
		DefaultTicket:   cfg.Ticket.DefaultTicket,
	}

	rng, err := timerange.Resolve(*month, *from, *to, *until, time.Now())
	if err != nil {
		return fail(err)
	}

	var rep report.Report
	if command == "sync" {
		rep, err = application.Sync(ctx, rng, *apply)
	} else {
		rep, err = application.Validate(ctx, rng)
	}
	if err != nil {
		return fail(err)
	}

	if err := writeReport(os.Stdout, rep, *asJSON); err != nil {
		return fail(fmt.Errorf("Ausgabe fehlgeschlagen: %w", err))
	}
	return rep.ExitCode()
}

func writeReport(w io.Writer, rep report.Report, asJSON bool) error {
	if asJSON {
		return rep.WriteJSON(w)
	}
	return rep.WriteText(w)
}

func fail(err error) int {
	fmt.Fprintf(os.Stderr, "Fehler: %v\n", err)
	return 2
}

// doctor prüft beide Systeme. Die Tempo-Sonden dürfen fehlschlagen, ohne dass
// das Tool unbrauchbar wäre — sie beantworten nur, welcher jira.target-Wert
// bei diesem Kunden langfristig passt.
func doctor(ctx context.Context, cfg *config.Config, httpClient *http.Client) int {
	checks := doctorpkg.Run(ctx, cfg, httpClient, "")
	if err := doctorpkg.WriteChecks(os.Stdout, checks); err != nil {
		return fail(fmt.Errorf("Ausgabe fehlgeschlagen: %w", err))
	}
	for _, c := range checks {
		if !c.OK && (c.Name == "Moco erreichbar" || c.Name == "Jira erreichbar") {
			return 1
		}
	}
	return 0
}
