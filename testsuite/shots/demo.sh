#!/bin/sh
# Makes the demo projects the tutorial's screenshots are taken in, and the
# folders that keep the y started by start.sh apart from yours.
#
#   ./demo.sh          make them (again: it starts over)
#   ./demo.sh --clean  remove them, and the demo sessions' transcripts
#
# DEMO is where the projects go (~/code by default): tidepool, a small Go
# tool for tide times, and field-notes, a few Markdown notes. $DEMO/.shots
# holds y's settings and state, and its views of Claude Code and Codex.
set -eu
DEMO=${DEMO:-$HOME/code}
H=$DEMO/.shots

slug() { printf '%s' "$1" | sed 's/[^A-Za-z0-9]/-/g'; }

clean() {
	for p in tidepool field-notes; do
		dir=$HOME/.claude/projects/$(slug "$DEMO/$p")
		for t in "$dir"/*.jsonl; do
			[ -e "$t" ] || continue
			rm -rf "$HOME/.claude/file-history/$(basename "$t" .jsonl)"
		done
		rm -rf "$dir"
	done
	rm -rf "$DEMO/tidepool" "$DEMO/field-notes" "$H"
	rmdir "$DEMO" 2>/dev/null || true
}

if [ "${1:-}" = "--clean" ]; then
	clean
	echo "removed the demo projects, $H and their transcripts"
	exit 0
fi
clean

T=$DEMO/tidepool
mkdir -p "$T/cmd/tidepool" "$T/internal/tides" "$T/internal/api" "$T/data" "$T/docs"
cat >"$T/go.mod" <<'EOF'
module github.com/example/tidepool

go 1.23
EOF
cat >"$T/README.md" <<'EOF'
# tidepool

Tide times for the stations you care about, in your terminal or over HTTP.

```
$ tidepool today monterey
05:12  low   0.4 ft
11:38  high  4.9 ft
17:02  low   1.1 ft
23:21  high  5.6 ft
```

## Install

Download a release from the releases page and put it on your PATH.

## Usage

    tidepool today <station>     today's highs and lows
    tidepool week <station>      the next seven days
    tidepool serve --port 8080   the same, as JSON over HTTP

Stations live in `data/stations.csv`.
EOF
cat >"$T/cmd/tidepool/main.go" <<'EOF'
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/example/tidepool/internal/api"
	"github.com/example/tidepool/internal/tides"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	stations, err := tides.LoadStations("data/stations.csv")
	if err != nil {
		fmt.Fprintln(os.Stderr, "tidepool:", err)
		os.Exit(1)
	}
	switch os.Args[1] {
	case "today":
		printDays(stations, os.Args[2:], 1)
	case "week":
		printDays(stations, os.Args[2:], 7)
	case "serve":
		if err := api.Serve(stations, ":8080"); err != nil {
			fmt.Fprintln(os.Stderr, "tidepool:", err)
			os.Exit(1)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func printDays(stations tides.Stations, args []string, days int) {
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	st, ok := stations.Find(args[0])
	if !ok {
		fmt.Fprintf(os.Stderr, "tidepool: no station %q\n", args[0])
		os.Exit(1)
	}
	for _, e := range tides.Predict(st, time.Now(), days) {
		fmt.Printf("%s  %-5s %4.1f ft\n", e.At.Format("15:04"), e.Kind, e.Height)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: tidepool today|week <station> | serve")
}
EOF
cat >"$T/internal/tides/station.go" <<'EOF'
package tides

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Station is a tide gauge and the harmonic constants that predict it.
type Station struct {
	ID     string
	Name   string
	Lat    float64
	Lon    float64
	Datum  float64 // mean lower low water, in feet
	Consts []Constituent
}

type Stations []Station

// Find matches a station by ID or by a case-insensitive name prefix.
func (s Stations) Find(q string) (Station, bool) {
	q = strings.ToLower(q)
	for _, st := range s {
		if st.ID == q || strings.HasPrefix(strings.ToLower(st.Name), q) {
			return st, true
		}
	}
	return Station{}, false
}

// LoadStations reads stations.csv: id, name, lat, lon, datum, then
// name:amplitude:phase triples for each constituent.
func LoadStations(path string) (Stations, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	var out Stations
	for i, row := range rows[1:] {
		st, err := parseStation(row)
		if err != nil {
			return nil, fmt.Errorf("stations.csv line %d: %w", i+2, err)
		}
		out = append(out, st)
	}
	return out, nil
}

func parseStation(row []string) (Station, error) {
	lat, _ := strconv.ParseFloat(row[2], 64)
	lon, _ := strconv.ParseFloat(row[3], 64)
	datum, _ := strconv.ParseFloat(row[4], 64)
	st := Station{ID: row[0], Name: row[1], Lat: lat, Lon: lon, Datum: datum}
	for _, c := range row[5:] {
		parts := strings.Split(c, ":")
		amp, _ := strconv.ParseFloat(parts[1], 64)
		phase, _ := strconv.ParseFloat(parts[2], 64)
		st.Consts = append(st.Consts, Constituent{Name: parts[0], Amplitude: amp, Phase: phase})
	}
	return st, nil
}
EOF
cat >"$T/internal/tides/predict.go" <<'EOF'
package tides

import (
	"math"
	"time"
)

// Constituent is one harmonic term: a cosine with its own speed.
type Constituent struct {
	Name      string
	Amplitude float64 // feet
	Phase     float64 // degrees
}

// Speeds in degrees per hour.
var speeds = map[string]float64{
	"M2": 28.9841042, "S2": 30.0, "N2": 28.4397295,
	"K1": 15.0410686, "O1": 13.9430356,
}

// Event is a high or low water.
type Event struct {
	At     time.Time
	Kind   string
	Height float64
}

// Height is the predicted water level at t, in feet above the datum.
func Height(st Station, t time.Time) float64 {
	hours := t.Sub(time.Date(t.Year(), 1, 1, 0, 0, 0, 0, time.UTC)).Hours()
	h := st.Datum
	for _, c := range st.Consts {
		h += c.Amplitude * math.Cos((speeds[c.Name]*hours-c.Phase)*math.Pi/180)
	}
	return h
}

// Predict finds the highs and lows in the days starting at from's midnight,
// stepping a minute at a time.
func Predict(st Station, from time.Time, days int) []Event {
	start := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())
	var events []Event
	prev, cur := Height(st, start.Add(-time.Minute)), Height(st, start)
	for m := 1; m <= days*24*60; m++ {
		t := start.Add(time.Duration(m) * time.Minute)
		next := Height(st, t)
		switch {
		case cur > prev && cur >= next:
			events = append(events, Event{t.Add(-time.Minute), "high", cur})
		case cur < prev && cur <= next:
			events = append(events, Event{t.Add(-time.Minute), "low", cur})
		}
		prev, cur = cur, next
	}
	return events
}
EOF
cat >"$T/internal/tides/predict_test.go" <<'EOF'
package tides

import (
	"testing"
	"time"
)

func TestPredictAlternates(t *testing.T) {
	st := Station{Datum: 3, Consts: []Constituent{{"M2", 2, 0}}}
	events := Predict(st, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), 1)
	if len(events) < 3 {
		t.Fatalf("got %d events, want at least 3", len(events))
	}
	for i := 1; i < len(events); i++ {
		if events[i].Kind == events[i-1].Kind {
			t.Errorf("events %d and %d are both %s", i-1, i, events[i].Kind)
		}
	}
}
EOF
cat >"$T/internal/api/server.go" <<'EOF'
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/example/tidepool/internal/tides"
)

// Serve answers GET /tides/{station}?days=N with the events as JSON.
func Serve(stations tides.Stations, addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tides/{station}", func(w http.ResponseWriter, r *http.Request) {
		st, ok := stations.Find(r.PathValue("station"))
		if !ok {
			http.Error(w, "no such station", http.StatusNotFound)
			return
		}
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		if days == 0 {
			days = 1
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tides.Predict(st, time.Now(), days))
	})
	return http.ListenAndServe(addr, mux)
}
EOF
cat >"$T/data/stations.csv" <<'EOF'
id,name,lat,lon,datum,constituents
9413450,Monterey,36.6089,-121.8914,2.83,M2:1.62:181.3,S2:0.43:186.9,N2:0.36:156.2,K1:1.15:219.4,O1:0.72:202.6
9414290,San Francisco,37.8063,-122.4659,3.12,M2:1.80:194.6,S2:0.41:200.1,N2:0.39:171.8,K1:1.21:226.3,O1:0.76:209.9
9410170,San Diego,32.7142,-117.1736,2.79,M2:1.71:148.9,S2:0.68:144.2,N2:0.40:132.5,K1:1.10:197.4,O1:0.69:183.0
8443970,Boston,42.3539,-71.0503,5.15,M2:4.53:110.6,S2:0.70:150.3,N2:1.05:80.2,K1:0.45:200.8,O1:0.37:179.0
EOF
cat >"$T/docs/design.md" <<'EOF'
# Design notes

Predictions sum five harmonic constituents (M2, S2, N2, K1, O1). That is
within about 0.3 ft of NOAA's published tables for the stations we carry
(estimated from a week of spot checks, not measured systematically).

## Open questions

- Daylight saving: events near 2 a.m. on the switch days print twice.
- The HTTP server has no timeouts yet.
- Station search matches name prefixes only ("monterey" works, "bay" does not).
EOF
cat >"$T/docs/roadmap.md" <<'EOF'
# Roadmap

1. `tidepool serve`: timeouts, a health check, and a JSON error body.
2. `tidepool week`: group events by day.
3. More stations from the NOAA list.
EOF
printf '/tidepool\n' >"$T/.gitignore"
git -C "$T" init -q
git -C "$T" add -A
git -C "$T" -c user.name=demo -c user.email=demo@example.com commit -q -m "tidepool: predictions, CLI, HTTP"

F=$DEMO/field-notes
mkdir -p "$F/2026-11" "$F/photos"
cat >"$F/2026-11/03-monterey.md" <<'EOF'
# Monterey, Nov 3

Low at 05:12 (0.4 ft). Tide pools at Point Pinos open by 04:40.

- Ochre stars on the north rocks, about a dozen
- Two nudibranchs (Hermissenda) in the shallow pool by the stairs
- Fog until 08:00; bring the red headlamp next time
EOF
cat >"$F/2026-11/06-pescadero.md" <<'EOF'
# Pescadero, Nov 6

Low at 06:48 (-0.2 ft), the best of the month.

- Anemones everywhere on the reef shelf
- A sea hare, 20 cm, near the outflow
- tidepool said 06:51; the gauge said 06:48
EOF
cat >"$F/2026-11/plan-weekend.md" <<'EOF'
# This weekend

| day | spot        | low   | height |
|-----|-------------|-------|--------|
| Sat | Duxbury     | 06:02 | -0.4   |
| Sun | Fitzgerald  | 06:41 | -0.1   |

Pack: boots, headlamp, tide card, thermos.
EOF
cat >"$F/checklist.md" <<'EOF'
# Checklist

- [x] boots
- [x] headlamp
- [ ] new tide card for December
- [ ] print the Duxbury map
EOF
cat >"$F/sightings.csv" <<'EOF'
date,place,species,count
2026-11-03,Monterey,Pisaster ochraceus,12
2026-11-03,Monterey,Hermissenda opalescens,2
2026-11-06,Pescadero,Anthopleura elegantissima,40
2026-11-06,Pescadero,Aplysia californica,1
EOF

# y sees only the demo's Claude Code transcripts (and the backups W diffs
# against), not every project of yours.
mkdir -p "$H/claude/projects"
for p in tidepool field-notes; do
	s=$(slug "$DEMO/$p")
	mkdir -p "$HOME/.claude/projects/$s"
	ln -s "$HOME/.claude/projects/$s" "$H/claude/projects/$s"
done
ln -s "$HOME/.claude/file-history" "$H/claude/file-history"

# Codex runs in a home of its own, signed in through your auth file. It must
# stay short: Codex's socket path in it can't pass 104 bytes.
mkdir -p "$H/codex"
ln -s "$HOME/.codex/auth.json" "$H/codex/auth.json"
[ ! -e "$HOME/.codex/models_cache.json" ] || ln -s "$HOME/.codex/models_cache.json" "$H/codex/models_cache.json"
printf '[projects."%s"]\ntrust_level = "trusted"\n' "$T" >"$H/codex/config.toml"

# Pinned folders, recent files and zoxide's places, for the s and z popups.
mkdir -p "$H/xdg/data/marmota" "$H/xdg/state" "$H/xdg/cache" "$H/xdg/config" "$H/zoxide"
now=$(date +%s)
cat >"$H/xdg/data/marmota/pinned.json" <<EOF
[{"location": "$T", "name": "tidepool"}, {"location": "$F", "name": "field-notes"}, {"location": "$T/docs", "name": "docs"}]
EOF
cat >"$H/xdg/data/marmota/recent_edits.json" <<EOF
[{"location": "$T/internal/tides/predict.go", "name": "predict.go", "edited_at": $((now - 600))},
 {"location": "$F/2026-11/plan-weekend.md", "name": "plan-weekend.md", "edited_at": $((now - 3600))}]
EOF
if command -v zoxide >/dev/null; then
	for d in "$T" "$T" "$T" "$T/internal/tides" "$T/docs" "$F" "$F/2026-11"; do
		_ZO_DATA_DIR=$H/zoxide zoxide add "$d"
	done
fi
echo "made $T and $F; start y there with ./start.sh"
