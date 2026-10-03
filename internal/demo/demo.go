// Package demo simulates a Prometheus exporter with the metric set of the
// promtop design mock. It is deterministic for a given seed and backs the UI
// golden tests and cmd/fakeexporter.
package demo

import (
	"fmt"
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
)

type wave struct {
	p, l int
	a    float64 // spike amplitude (gauges) or burst multiplier (counters)
}

type sim struct {
	name, help, typ string // typ: # TYPE word
	labels          [][2]string
	counter         bool
	base, noise     float64
	rate            float64
	integer         bool
	floor           bool // clamp gauges at 0
	spike, burst    *wave
	saw             int
	v, r            float64
}

var histLE = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, math.Inf(1)}

// Target is a simulated exporter.
type Target struct {
	rng      *rand.Rand
	series   []*sim
	tenants  []*sim
	cum      []float64
	hsum     float64
	hcount   float64
	gcCount  float64
	t        int
	start    float64
	highCard bool
	empty    bool
}

// New returns a Target seeded for reproducible output.
func New(seed uint64) *Target {
	g := &Target{rng: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)), start: 1790000000, //nolint:gosec // deterministic simulation, not security-sensitive
		cum: make([]float64, len(histLE)), gcCount: 48211}
	g.series = g.makeSeries()
	return g
}

func (g *Target) gauss() float64 {
	u, v := 0.0, 0.0
	for u == 0 {
		u = g.rng.Float64()
	}
	for v == 0 {
		v = g.rng.Float64()
	}
	return math.Sqrt(-2*math.Log(u)) * math.Cos(2*math.Pi*v)
}

func (g *Target) makeSeries() []*sim {
	var out []*sim
	add := func(s *sim) {
		switch {
		case s.counter:
			s.typ, s.r = "counter", s.rate
		default:
			if s.typ == "" {
				s.typ = "gauge"
			}
			s.v = s.base
		}
		out = append(out, s)
	}
	lbl := func(kv ...string) [][2]string {
		var ls [][2]string
		for i := 0; i+1 < len(kv); i += 2 {
			ls = append(ls, [2]string{kv[i], kv[i+1]})
		}
		return ls
	}
	add(&sim{name: "up", help: "1 if the target is reachable, 0 otherwise.", base: 1, integer: true})
	add(&sim{name: "go_goroutines", help: "Number of goroutines that currently exist.", base: 220, noise: 9, integer: true, spike: &wave{240, 45, 95}})
	add(&sim{name: "go_threads", help: "Number of OS threads created.", base: 18, noise: 0.6, integer: true})
	add(&sim{name: "go_memstats_heap_alloc_bytes", help: "Bytes of allocated heap objects.", base: 84e6, noise: 1.5e6, integer: true, saw: 45})
	add(&sim{name: "process_resident_memory_bytes", help: "Resident memory size in bytes.", base: 212e6, noise: 1.2e6, integer: true})
	add(&sim{name: "process_cpu_seconds_total", help: "Total user and system CPU time spent in seconds.", counter: true, rate: 0.42, v: 3812.4})
	add(&sim{name: "process_open_fds", help: "Number of open file descriptors.", base: 64, noise: 1.5, integer: true})
	for _, c := range []struct {
		m, code string
		r       float64
		b       *wave
	}{{"GET", "200", 182, nil}, {"GET", "404", 6, nil}, {"GET", "500", 0.6, &wave{200, 25, 8}}, {"POST", "200", 42, nil},
		{"POST", "400", 2.5, nil}, {"POST", "500", 0.3, nil}, {"PUT", "200", 9, nil}, {"DELETE", "204", 3, nil}} {
		add(&sim{name: "http_requests_total", help: "Total HTTP requests processed, by method and status code.", counter: true,
			labels: lbl("method", c.m, "code", c.code), rate: c.r, integer: true, burst: c.b,
			v: math.Round(c.r * 86400 * (0.8 + g.rng.Float64()*0.4))})
	}
	add(&sim{name: "http_inflight_requests", help: "Requests currently being served.", base: 14, noise: 3, integer: true, floor: true})
	add(&sim{name: "db_pool_connections", help: "Database pool connections by state.", labels: lbl("state", "active"), base: 18, noise: 2, integer: true, floor: true})
	add(&sim{name: "db_pool_connections", help: "Database pool connections by state.", labels: lbl("state", "idle"), base: 12, noise: 2, integer: true, floor: true})
	add(&sim{name: "cache_hits_total", help: "Cache lookups that hit.", counter: true, rate: 950, integer: true, v: 81234567})
	add(&sim{name: "cache_misses_total", help: "Cache lookups that missed.", counter: true, rate: 70, integer: true, v: 6012345})
	add(&sim{name: "queue_depth", help: "Jobs waiting per queue.", labels: lbl("queue", "emails"), base: 38, noise: 4, integer: true, floor: true, spike: &wave{300, 70, 48}})
	add(&sim{name: "queue_depth", help: "Jobs waiting per queue.", labels: lbl("queue", "webhooks"), base: 5, noise: 1.5, integer: true, floor: true})
	add(&sim{name: "node_load1", help: "1m load average.", base: 1.8, noise: 0.08})
	add(&sim{name: "node_load5", help: "5m load average.", base: 1.6, noise: 0.03})
	add(&sim{name: "node_load15", help: "15m load average.", base: 1.5, noise: 0.015})
	for _, q := range []struct {
		q string
		b float64
	}{{"0", 0.00021}, {"0.25", 0.00038}, {"0.5", 0.00052}, {"0.75", 0.00081}, {"1", 0.0034}} {
		add(&sim{name: "go_gc_duration_seconds", typ: "summary", help: "A summary of the pause duration of GC cycles.",
			labels: lbl("quantile", q.q), base: q.b, noise: q.b * 0.06, floor: true})
	}
	add(&sim{name: "go_memstats_sys_bytes", help: "Bytes of memory obtained from the OS.", base: 142e6, noise: 4e5, integer: true})
	add(&sim{name: "go_memstats_stack_inuse_bytes", help: "Bytes in use by the stack allocator.", base: 3.2e6, noise: 6e4, integer: true})
	add(&sim{name: "go_memstats_heap_objects", help: "Number of allocated objects.", base: 412e3, noise: 9e3, integer: true, saw: 45})
	add(&sim{name: "go_memstats_mallocs_total", help: "Total number of mallocs.", counter: true, rate: 18500, integer: true, v: 9.1e9})
	add(&sim{name: "go_memstats_frees_total", help: "Total number of frees.", counter: true, rate: 18300, integer: true, v: 9.08e9})
	add(&sim{name: "go_gc_cycles_total", help: "Completed GC cycles.", counter: true, rate: 0.022, integer: true, v: 48211})
	for _, cpu := range []string{"0", "1", "2", "3"} {
		for _, md := range []struct {
			m string
			r float64
		}{{"user", 0.31}, {"system", 0.09}, {"iowait", 0.02}, {"idle", 0.56}} {
			add(&sim{name: "node_cpu_seconds_total", help: "Seconds the CPUs spent in each mode.", counter: true,
				labels: lbl("cpu", cpu, "mode", md.m), rate: md.r * (0.85 + g.rng.Float64()*0.3), v: math.Round(md.r * 3.2e6)})
		}
	}
	add(&sim{name: "node_memory_MemAvailable_bytes", help: "Memory available in bytes.", base: 5.4e9, noise: 1.2e7, integer: true})
	add(&sim{name: "node_memory_MemTotal_bytes", help: "Memory total in bytes.", base: 8589934592, integer: true})
	for _, fs := range []struct {
		m string
		b float64
	}{{"/", 21.4e9}, {"/data", 188e9}} {
		add(&sim{name: "node_filesystem_avail_bytes", help: "Filesystem space available in bytes.", labels: lbl("mountpoint", fs.m), base: fs.b, noise: fs.b * 0.0004, integer: true})
	}
	for _, d := range []struct {
		dev string
		r   float64
	}{{"eth0", 2.1e6}, {"lo", 3.8e5}} {
		add(&sim{name: "node_network_receive_bytes_total", help: "Network bytes received.", counter: true, labels: lbl("device", d.dev), rate: d.r, integer: true, v: d.r * 9e5})
		add(&sim{name: "node_network_transmit_bytes_total", help: "Network bytes transmitted.", counter: true, labels: lbl("device", d.dev), rate: d.r * 1.7, integer: true, v: d.r * 1.5e6})
	}
	add(&sim{name: "node_disk_read_bytes_total", help: "Bytes read from disk.", counter: true, labels: lbl("device", "nvme0n1"), rate: 4.2e5, integer: true, v: 3.1e11})
	add(&sim{name: "node_disk_written_bytes_total", help: "Bytes written to disk.", counter: true, labels: lbl("device", "nvme0n1"), rate: 1.3e6, integer: true, v: 8.7e11})
	for _, gm := range []struct {
		m string
		r float64
	}{{"GetUser", 64}, {"ListOrders", 22}, {"CreateOrder", 5.5}, {"CancelOrder", 0.4}} {
		add(&sim{name: "grpc_server_handled_total", help: "RPCs completed on the server, by method and code.", counter: true,
			labels: lbl("grpc_method", gm.m, "grpc_code", "OK"), rate: gm.r, integer: true, v: math.Round(gm.r * 7e4)})
		add(&sim{name: "grpc_server_handled_total", help: "RPCs completed on the server, by method and code.", counter: true,
			labels: lbl("grpc_method", gm.m, "grpc_code", "Unavailable"), rate: gm.r * 0.004, integer: true, v: math.Round(gm.r * 300)})
	}
	for _, op := range []struct {
		op string
		r  float64
	}{{"select", 310}, {"insert", 24}, {"update", 17}, {"delete", 2.2}} {
		add(&sim{name: "db_queries_total", help: "Database queries executed, by operation.", counter: true, labels: lbl("op", op.op), rate: op.r, integer: true, v: math.Round(op.r * 8.6e4)})
	}
	for i, pt := range []string{"0", "1", "2"} {
		add(&sim{name: "kafka_consumer_lag", help: "Consumer group lag per partition.", labels: lbl("topic", "orders", "partition", pt),
			base: []float64{120, 85, 410}[i], noise: 18, integer: true, floor: true})
	}
	add(&sim{name: "cache_items", help: "Items currently held in the cache.", base: 48210, noise: 120, integer: true})
	add(&sim{name: "cache_evictions_total", help: "Cache evictions.", counter: true, rate: 11, integer: true, v: 1823311})
	for i, q := range []string{"emails", "webhooks"} {
		add(&sim{name: "queue_processed_total", help: "Jobs processed per queue.", counter: true, labels: lbl("queue", q),
			rate: []float64{14, 31}[i], integer: true, v: []float64{4.1e6, 9.8e6}[i]})
	}
	add(&sim{name: "kube_pod_container_status_restarts_total", help: "Number of container restarts per container.", counter: true,
		labels: lbl("cluster", "prod-eu-1", "region", "eu-central-1", "zone", "eu-central-1b", "namespace", "payments",
			"pod", "payments-api-7c9f8d6b54-x2kqz", "container", "api", "image", "registry.internal/payments/api:2.14.3",
			"image_id", "sha256:4f1c9e0a7d2b", "container_id", "containerd://9b1e44c07a3f", "node", "ip-10-0-3-14.ec2.internal",
			"host_ip", "10.0.3.14", "pod_ip", "10.42.7.118", "uid", "e3b0c442-98fc-4c14-9afb-f4c8996fb924",
			"created_by_kind", "ReplicaSet", "created_by_name", "payments-api-7c9f8d6b54", "service_account", "payments-api",
			"priority_class", "high-priority", "qos_class", "Burstable", "team", "checkout", "env", "production"),
		rate: 0.004, integer: true, v: 3})
	add(&sim{name: "build_info", help: "Build metadata, value is always 1.", labels: lbl("version", "2.14.3", "goversion", "go1.23.4"), base: 1, integer: true})
	return out
}

// Step advances the simulation by one scrape interval and renders it.
func (g *Target) Step() string {
	g.t++
	for _, s := range g.series {
		g.stepSeries(s)
	}
	if g.highCard {
		for _, s := range g.tenants {
			if g.rng.Float64() < s.rate {
				s.v++
			}
		}
	}
	g.stepHist()
	g.gcCount += 0.022
	return g.Render()
}

func (g *Target) stepSeries(s *sim) {
	if s.counter {
		tg := s.rate
		if s.burst != nil && g.t%s.burst.p < s.burst.l {
			tg *= s.burst.a
		}
		s.r = math.Max(0, s.r+(tg-s.r)*0.25+g.gauss()*s.rate*0.08)
		inc := s.r
		if s.integer {
			inc = math.Floor(s.r)
			if g.rng.Float64() < math.Mod(s.r, 1) {
				inc++
			}
		}
		s.v += inc
		return
	}
	tg, k := s.base, 0.15
	if s.spike != nil && g.t%s.spike.p < s.spike.l {
		tg += s.spike.a
	}
	if s.saw > 0 {
		tg, k = s.base*(0.75+0.5*float64(g.t%s.saw)/float64(s.saw)), 0.5
	}
	s.v += (tg-s.v)*k + g.gauss()*s.noise
	if s.integer {
		s.v = math.Round(s.v)
	}
	if s.floor {
		s.v = math.Max(0, s.v)
	}
}

func (g *Target) stepHist() {
	n := int(math.Round(230 + g.gauss()*15))
	slow := g.t%180 < 22
	b := make([]float64, len(histLE))
	for range n {
		mu, sd := math.Log(0.045), 0.7
		if slow && g.rng.Float64() < 0.3 {
			mu, sd = math.Log(0.6), 0.8
		}
		x := math.Exp(mu + sd*g.gauss())
		k := 0
		for x > histLE[k] {
			k++
		}
		b[k]++
		g.hsum += x
	}
	c := 0.0
	for i, x := range b {
		c += x
		g.cum[i] += c
	}
	g.hcount += float64(n)
}

// Restart simulates a target restart: counters start over and
// process_start_time_seconds changes.
func (g *Target) Restart() {
	for _, s := range g.series {
		if s.counter {
			s.v = s.rate * 3
			if s.integer {
				s.v = math.Round(s.v)
			}
		}
		if s.name == "go_goroutines" {
			s.v = 40
		}
	}
	clear(g.cum)
	g.hsum, g.hcount, g.gcCount = 0, 0, 0
	g.start += float64(g.t)
}

// SetHighCardinality adds (or hides) 20,000 tenant_api_requests_total series.
func (g *Target) SetHighCardinality(on bool) {
	g.highCard = on
	if !on || g.tenants != nil {
		return
	}
	eps := []string{"/v1/orders", "/v1/users", "/v1/cart", "/v1/search", "/v1/payments"}
	for i := range 20000 {
		g.tenants = append(g.tenants, &sim{name: "tenant_api_requests_total",
			labels:  [][2]string{{"tenant", fmt.Sprintf("t-%04d", i%240)}, {"endpoint", eps[i%5]}, {"user_id", fmt.Sprintf("u-%05d", i)}},
			counter: true, rate: 0.02 + g.rng.Float64()*0.3, v: math.Round(g.rng.Float64() * 5000)})
	}
}

// SetEmpty makes the target answer with an empty payload.
func (g *Target) SetEmpty(on bool) { g.empty = on }

// Render returns the exposition text for the current instant.
func (g *Target) Render() string {
	if g.empty {
		return ""
	}
	var b strings.Builder
	var order []string
	fams := map[string][]*sim{}
	for _, s := range g.series {
		if fams[s.name] == nil {
			order = append(order, s.name)
		}
		fams[s.name] = append(fams[s.name], s)
	}
	for _, name := range order {
		ss := fams[name]
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n", name, ss[0].help, name, ss[0].typ)
		for _, s := range ss {
			writeSample(&b, name, s.labels, s.v)
		}
		if ss[0].typ == "summary" {
			writeSample(&b, name+"_sum", nil, math.Round(g.gcCount*0.0006*1e4)/1e4)
			writeSample(&b, name+"_count", nil, math.Floor(g.gcCount))
		}
		if name == "http_requests_total" {
			g.writeHist(&b)
		}
	}
	b.WriteString("# HELP process_start_time_seconds Start time of the process since unix epoch in seconds.\n# TYPE process_start_time_seconds gauge\n")
	writeSample(&b, "process_start_time_seconds", nil, g.start)
	if g.highCard {
		b.WriteString("# HELP tenant_api_requests_total API requests per tenant, endpoint and user.\n# TYPE tenant_api_requests_total counter\n")
		for _, s := range g.tenants {
			writeSample(&b, s.name, s.labels, s.v)
		}
	}
	return b.String()
}

func (g *Target) writeHist(b *strings.Builder) {
	const name = "http_request_duration_seconds"
	fmt.Fprintf(b, "# HELP %s HTTP request latency in seconds.\n# TYPE %s histogram\n", name, name)
	for i, le := range histLE {
		ls := "+Inf"
		if !math.IsInf(le, 1) {
			ls = strconv.FormatFloat(le, 'f', -1, 64)
		}
		writeSample(b, name+"_bucket", [][2]string{{"le", ls}}, g.cum[i])
	}
	writeSample(b, name+"_sum", nil, g.hsum)
	writeSample(b, name+"_count", nil, g.hcount)
}

func writeSample(b *strings.Builder, name string, labels [][2]string, v float64) {
	b.WriteString(name)
	if len(labels) > 0 {
		b.WriteByte('{')
		for i, l := range labels {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(b, "%s=%q", l[0], l[1])
		}
		b.WriteByte('}')
	}
	b.WriteByte(' ')
	b.WriteString(num(v))
	b.WriteByte('\n')
}

func num(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return strconv.FormatFloat(math.Round(v*1e4)/1e4, 'f', -1, 64)
}
