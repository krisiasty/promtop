# promtop

Terminal UI (TUI) for a single Prometheus endpoint. It scrapes the endpoint at a fixed interval,
keeps up to 30 minutes of history in memory and shows it in six views:
**Table, History, Graph, Heatmap, Series, Raw**.

## Install

**Homebrew (macOS):**

```sh
brew install --cask krisiasty/tap/promtop
```

**Linux packages:** download the `.deb`, `.rpm` or `.apk` for your
architecture from the [latest release](https://github.com/krisiasty/promtop/releases/latest):

```sh
sudo dpkg -i promtop_<version>_linux_amd64.deb                 # Debian, Ubuntu
sudo rpm -i promtop_<version>_linux_amd64.rpm                  # Fedora, RHEL
sudo apk add --allow-untrusted promtop_<version>_linux_amd64.apk  # Alpine
```

**Binaries:** every release also has a plain binary for macOS, Linux and
Windows on amd64 and arm64, plus `checksums.txt`.

**From source** (Go 1.27.1 or newer):

```sh
go install github.com/krisiasty/promtop/cmd/promtop@latest
```

The binary is pure Go and runs on macOS, Linux and Windows. On Windows use
**Windows Terminal**; the legacy console (`conhost`) lacks glyphs for braille
charts and block bars.

## Usage

```sh
promtop http://10.0.3.14:9100/metrics
promtop --interval 2s --window 15m --match 'http_.*' https://app.internal:8443/metrics
promtop --history-budget 2GiB https://large.internal:8443/metrics
```

The URL is used exactly as given: include the scheme (`http://` or
`https://`); no path is added. Query strings are passed through (useful for
blackbox or SNMP exporters). Credentials embedded in the URL
(`http://user:pass@host/…`) are rejected — use `--username` with
`--password-file` (or `$PROMTOP_PASSWORD`) instead.

Scrapes using basic or bearer authentication reject HTTP redirects so credentials
cannot be forwarded to another endpoint.

| Flag | Default | |
| --- | --- | --- |
| `--url` | | endpoint (instead of the positional URL) |
| `--interval` | `1s` | scrape interval (≥ 100ms) |
| `--window` | `5m` | initial stats window: `30s`, `1m`, `2m`, `5m`, `10m`, `15m`, `20m`, `30m` |
| `--history-budget` | `512MiB` | estimated ring memory limit; accepts `MiB`, `GiB`, `MB`, or `GB` |
| `--timeout` | `2s` | per-scrape timeout |
| `--match` | | keep only families whose name matches this regexp |
| `--username` | | basic auth user; password from `--password-file` or `$PROMTOP_PASSWORD` |
| `--password-file` | | file holding the basic auth password |
| `--bearer-token-file` | | bearer token file (or `$PROMTOP_BEARER_TOKEN`) |
| `--ca-file`, `--cert-file`, `--key-file` | | TLS CA bundle and client certificate |
| `--insecure-skip-verify` | | skip server certificate verification |
| `--version` | | print version, commit and build date |

## Keys

| Key | Action |
| --- | --- |
| `1`–`6`, `← →`, `tab` / `shift+tab` | switch view |
| `↑ ↓`, `g` / `G` | move / top / bottom (History and Raw: scroll; Heatmap: histogram; Series: family) |
| `PgUp` / `PgDn` | move a page in scrollable lists and details |
| `[` / `]` | History: previous / next series |
| `space` | pin / unpin the selected series (pins last for the session) |
| `enter` | series details (Table, History, Graph; `↑ ↓` scroll labels or buckets); open family (Series, narrow) |
| `esc` | close popup → back to families → clear filter |
| `/` | filter by substring (enter keeps, esc clears) |
| `a` | all ↔ pinned only |
| `s` | sort: name, current, Δ%, p99 |
| `r` | counters as rate/s ↔ raw |
| `+` / `-` | increase / decrease the window (30s to 30m) |
| `w` | cycle window presets (Raw: toggle wrap) |
| `m` | Heatmap: by family ↔ by label set |
| `c` | Heatmap: bucket panels for the window ↔ since start |
| `e` | scrape error details (while scrapes fail) |
| `p` | pause / resume |
| `q` | quit |

The mouse works too: click tabs and rows, click the left margin to pin,
double-click a row for details, scroll with the wheel.

History lists every available value in the selected window, newest first. Scroll
the left list to see older values; its stats and distribution use the same
window's values, regardless of scroll position. The scrape interval controls
how often a new value can be added. For histograms, each History value is the
mean of observations since the preceding scrape.

History uses an estimated 512 MiB budget for ring buffers by default. With many
series or a short scrape interval, older values may fall out of a ring before
the selected window ends, including for pinned series. Increase the budget with
`--history-budget 1GiB` or `--history-budget 2GiB` when more memory is available.

## Development

```sh
go test ./...                                   # unit, golden and layout tests
go test ./internal/ui -run TestGolden -update   # rewrite golden screens after an intended change
golangci-lint run ./...                         # lint (config in .golangci.yml)
go run ./cmd/fakeexporter                       # simulated endpoint on :9100, control on :9101
curl 'http://127.0.0.1:9101/?scenario=refused'  # normal, refused, http503, slow, parse, empty, reset, highcard
goreleaser release --snapshot --clean           # build every release artifact locally into dist/
```

Golden screens live in `internal/ui/testdata/golden/`.

Releases are built by GoReleaser from a pushed `v*` tag (see
`.github/workflows/release.yml`).

## License

Apache License 2.0 — see [LICENSE](LICENSE) and [NOTICE](NOTICE).
