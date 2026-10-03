// Command fakeexporter serves a simulated Prometheus endpoint whose behaviour
// can be switched at runtime, to drive promtop through every design state.
//
//	go run ./cmd/fakeexporter
//	go run ./cmd/promtop http://127.0.0.1:9100/metrics
//	curl 'http://127.0.0.1:9101/?scenario=refused'
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/krisiasty/promtop/internal/demo"
)

var scenarios = []string{"normal", "refused", "http503", "slow", "parse", "empty", "reset", "highcard"}

type server struct {
	mu       sync.Mutex
	gen      *demo.Target
	scenario string
	addr     string
	srv      *http.Server
}

func main() {
	addr := flag.String("addr", "127.0.0.1:9100", "metrics listen address")
	ctl := flag.String("control", "127.0.0.1:9101", "control listen address")
	flag.Parse()
	s := &server{gen: demo.New(uint64(time.Now().UnixNano())), scenario: "normal", addr: *addr}
	go s.tick()
	if err := s.startMetrics(); err != nil {
		log.Fatal(err)
	}
	log.Printf("metrics: http://%s/metrics", *addr)
	log.Printf("control: http://%s/?scenario=NAME (%s)", *ctl, strings.Join(scenarios, ", "))
	ctlSrv := &http.Server{Addr: *ctl, Handler: http.HandlerFunc(s.control), ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(ctlSrv.ListenAndServe())
}

func (s *server) tick() {
	for range time.Tick(time.Second) {
		s.mu.Lock()
		s.gen.Step()
		s.mu.Unlock()
	}
}

func (s *server) startMetrics() error {
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", s.addr)
	if err != nil {
		return err
	}
	s.srv = &http.Server{Handler: http.HandlerFunc(s.metrics), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Print(err)
		}
	}()
	return nil
}

func (s *server) metrics(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	scenario, text := s.scenario, s.gen.Render()
	s.mu.Unlock()
	switch scenario {
	case "http503":
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	case "slow":
		time.Sleep(3 * time.Second)
	case "parse":
		text = text[:len(text)/2] + "broken{x=\""
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = io.WriteString(w, text)
}

func (s *server) control(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("scenario")
	s.mu.Lock()
	defer s.mu.Unlock()
	if name == "" {
		_, _ = fmt.Fprintf(w, "scenario=%s\navailable: %s\n", s.scenario, strings.Join(scenarios, ", "))
		return
	}
	switch name {
	case "normal", "refused", "http503", "slow", "parse", "empty", "highcard":
	case "reset":
		s.gen.Restart()
		name = "normal"
	default:
		http.Error(w, "unknown scenario; available: "+strings.Join(scenarios, ", "), http.StatusBadRequest)
		return
	}
	s.gen.SetEmpty(name == "empty")
	s.gen.SetHighCardinality(name == "highcard")
	switch {
	case name == "refused" && s.scenario != "refused":
		_ = s.srv.Close()
	case name != "refused" && s.scenario == "refused":
		if err := s.startMetrics(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	s.scenario = name
	_, _ = fmt.Fprintf(w, "scenario=%s\n", name) //nolint:gosec // name is one of the fixed scenario names checked above
}
