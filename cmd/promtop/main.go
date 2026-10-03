// Command promtop scrapes one Prometheus endpoint and explores it in a TUI.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/krisiasty/promtop/internal/config"
	"github.com/krisiasty/promtop/internal/scrape"
	"github.com/krisiasty/promtop/internal/ui"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	cfg, err := config.Parse(args, os.Getenv, os.ReadFile)
	switch {
	case errors.Is(err, flag.ErrHelp):
		config.Usage(stdout)
		return 0
	case err != nil:
		_, _ = fmt.Fprintln(stderr, "promtop:", err)
		config.Usage(stderr)
		return 2
	case cfg.ShowVersion:
		_, _ = fmt.Fprintf(stdout, "version: %s\ncommit: %s\ndate: %s\n", version, commit, date)
		return 0
	}
	client, err := scrape.NewClient(cfg.Target, cfg.Match, "promtop/"+version)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "promtop:", err)
		return 1
	}
	if _, err := tea.NewProgram(ui.New(cfg, ui.Options{Scrape: client.Scrape})).Run(); err != nil {
		_, _ = fmt.Fprintln(stderr, "promtop:", err)
		return 1
	}
	return 0
}
