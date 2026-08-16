package search

import (
	"sort"
	"strings"

	"github.com/mistweaverco/syncsh/internal/history"
)

type Result struct {
	Entry history.Entry
	Score int
}

const (
	classFuzzy     = 1
	classSubstring = 2
	classPrefix    = 3
	classWeight    = 1_000_000
)

func Rank(query string, entries []history.Entry, exact bool) []Result {
	q := strings.TrimSpace(query)
	out := make([]Result, 0, len(entries))
	if q == "" {
		for _, e := range entries {
			out = append(out, Result{Entry: e, Score: 0})
		}
		sort.SliceStable(out, func(i, j int) bool {
			return out[i].Entry.StartTS.After(out[j].Entry.StartTS)
		})
		return out
	}
	if exact {
		ql := strings.ToLower(q)
		for _, e := range entries {
			if strings.Contains(strings.ToLower(e.Command), ql) {
				out = append(out, Result{Entry: e, Score: 1})
			}
		}
		return out
	}
	for _, e := range entries {
		if score, ok := fuzzyScore(q, e.Command); ok {
			out = append(out, Result{Entry: e, Score: score})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Entry.StartTS.After(out[j].Entry.StartTS)
	})
	return out
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

// minSpan is the length of the shortest window of candidate that still
// contains query as a subsequence (Atuin's default fuzzy reorder).
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
