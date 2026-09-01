package search

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mistweaverco/syncsh/internal/history"
)

type Result struct {
	Entry history.Entry
	Score int
	Parts ScoreParts
}

type ScoreParts struct {
	Match     int
	Recency   int
	Frequency int
	Cwd       int
	Device    int
	Session   int
}

type Context struct {
	Now       time.Time
	Cwd       string
	DeviceID  string
	SessionID string
	Freq      map[string]int
}

const (
	classFuzzy     = 1
	classSubstring = 2
	classPrefix    = 3
	classWeight    = 1_000_000
	recencyWeight  = 200
	freqWeight     = 80
	cwdWeight      = 400
	deviceWeight   = 80
	sessionWeight  = 120
)

func Rank(query string, entries []history.Entry, exact bool) []Result {
	return RankWith(query, entries, exact, Context{})
}

func RankWith(query string, entries []history.Entry, exact bool, ctx Context) []Result {
	if ctx.Now.IsZero() {
		ctx.Now = time.Now().UTC()
	}
	if ctx.Freq == nil {
		ctx.Freq = Frequency(entries)
	}
	q := strings.TrimSpace(query)
	out := make([]Result, 0, len(entries))
	if q == "" {
		for _, e := range entries {
			r := Result{Entry: e}
			r.Parts = contextParts(e, ctx)
			r.Score = r.Parts.Recency + r.Parts.Frequency + r.Parts.Cwd + r.Parts.Device + r.Parts.Session
			out = append(out, r)
		}
		sortResults(out)
		return out
	}
	if exact {
		ql := strings.ToLower(q)
		for _, e := range entries {
			if strings.Contains(strings.ToLower(e.Command), ql) {
				r := Result{Entry: e, Score: 1, Parts: ScoreParts{Match: 1}}
				out = append(out, r)
			}
		}
		return out
	}
	for _, e := range entries {
		match, ok := fuzzyScore(q, e.Command)
		if !ok {
			continue
		}
		r := Result{Entry: e}
		r.Parts = contextParts(e, ctx)
		r.Parts.Match = match
		r.Score = r.Parts.Match + r.Parts.Recency + r.Parts.Frequency + r.Parts.Cwd + r.Parts.Device + r.Parts.Session
		out = append(out, r)
	}
	sortResults(out)
	return out
}

func BestSuggestion(prefix string, entries []history.Entry, ctx Context) string {
	s := Suggestions(prefix, entries, ctx, 1)
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

func Suggestions(prefix string, entries []history.Entry, ctx Context, limit int) []string {
	if limit <= 0 {
		limit = 8
	}
	ranked := RankWith(prefix, entries, false, ctx)
	seen := make(map[string]struct{}, len(ranked))
	out := make([]string, 0, limit)
	for _, r := range ranked {
		cmd := r.Entry.Command
		if cmd == prefix {
			continue
		}
		if _, ok := seen[cmd]; ok {
			continue
		}
		seen[cmd] = struct{}{}
		out = append(out, cmd)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func Explain(r Result) string {
	return fmt.Sprintf("score=%d match=%d recency=%d freq=%d cwd=%d device=%d session=%d cmd=%q",
		r.Score, r.Parts.Match, r.Parts.Recency, r.Parts.Frequency, r.Parts.Cwd, r.Parts.Device, r.Parts.Session, r.Entry.Command)
}

func Frequency(entries []history.Entry) map[string]int {
	m := map[string]int{}
	for _, e := range entries {
		m[e.Command]++
	}
	return m
}

func contextParts(e history.Entry, ctx Context) ScoreParts {
	var p ScoreParts
	age := ctx.Now.Sub(e.StartTS)
	if age < 0 {
		age = 0
	}
	days := int(age.Hours() / 24)
	rec := 30 - days
	if rec < 0 {
		rec = 0
	}
	p.Recency = rec * recencyWeight
	if ctx.Freq != nil {
		n := ctx.Freq[e.Command]
		if n > 20 {
			n = 20
		}
		p.Frequency = n * freqWeight
	}
	if ctx.Cwd != "" && e.Cwd == ctx.Cwd {
		p.Cwd = cwdWeight
	}
	if ctx.DeviceID != "" && e.DeviceID == ctx.DeviceID {
		p.Device = deviceWeight
	}
	if ctx.SessionID != "" && e.SessionID == ctx.SessionID {
		p.Session = sessionWeight
	}
	return p
}

func sortResults(out []Result) {
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Entry.StartTS.After(out[j].Entry.StartTS)
	})
}

func fuzzyScore(query, candidate string) (int, bool) {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return 0, true
	}
	cl := strings.ToLower(candidate)
	cr := []rune(cl)
	bestClass := classPrefix
	spanSum := 0
	for _, term := range terms {
		span, ok := minSpan([]rune(term), cr)
		if !ok {
			return 0, false
		}
		spanSum += span
		if c := matchClass(term, cl); c < bestClass {
			bestClass = c
		}
	}
	return bestClass*classWeight - spanSum, true
}

func matchClass(term, candidate string) int {
	if strings.HasPrefix(candidate, term) {
		return classPrefix
	}
	if strings.Contains(candidate, term) {
		return classSubstring
	}
	return classFuzzy
}

func minSpan(q, c []rune) (int, bool) {
	if len(q) == 0 {
		return 0, true
	}
	best := 0
	found := false
	for start := 0; start < len(c); start++ {
		if c[start] != q[0] {
			continue
		}
		qi := 1
		end := start
		for i := start + 1; i < len(c) && qi < len(q); i++ {
			if c[i] == q[qi] {
				end = i
				qi++
			}
		}
		if qi != len(q) {
			break
		}
		span := end - start + 1
		if !found || span < best {
			best = span
			found = true
		}
		if best == len(q) {
			return best, true
		}
	}
	return best, found
}
