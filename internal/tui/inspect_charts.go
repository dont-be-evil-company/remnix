package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dont-be-evil-company/remnix/internal/history"
)

type chartGranularity int

const (
	chartMonth chartGranularity = iota
	chartDay
	chartYear
)

func (g chartGranularity) Next() chartGranularity {
	return (g + 1) % 3
}

func (g chartGranularity) label() string {
	switch g {
	case chartDay:
		return "day"
	case chartYear:
		return "year"
	default:
		return "month"
	}
}

type chartBar struct {
	Label string
	Total int
	Fail  int
}

func bucketRuns(runs []history.Entry, g chartGranularity) []chartBar {
	if len(runs) == 0 {
		return nil
	}
	counts := map[string]*chartBar{}
	for _, e := range runs {
		key := bucketKey(e.StartTS, g)
		b, ok := counts[key]
		if !ok {
			b = &chartBar{Label: key}
			counts[key] = b
		}
		b.Total++
		if failed(e) {
			b.Fail++
		}
	}
	out := make([]chartBar, 0, len(counts))
	for _, b := range counts {
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

func bucketKey(t time.Time, g chartGranularity) string {
	u := t.UTC()
	switch g {
	case chartDay:
		return u.Format("2006-01-02")
	case chartYear:
		return u.Format("2006")
	default:
		return u.Format("2006-01")
	}
}

func topBy(runs []history.Entry, key func(history.Entry) string, n int) []chartBar {
	if n <= 0 || len(runs) == 0 {
		return nil
	}
	counts := map[string]*chartBar{}
	for _, e := range runs {
		k := key(e)
		if k == "" {
			k = "(none)"
		}
		b, ok := counts[k]
		if !ok {
			b = &chartBar{Label: k}
			counts[k] = b
		}
		b.Total++
		if failed(e) {
			b.Fail++
		}
	}
	out := make([]chartBar, 0, len(counts))
	for _, b := range counts {
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Total != out[j].Total {
			return out[i].Total > out[j].Total
		}
		return out[i].Label < out[j].Label
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func renderChartBlock(th Theme, runs []history.Entry, g chartGranularity, w, maxH int) string {
	if maxH < 4 || w < 16 || len(runs) == 0 {
		return ""
	}
	hostBars := topBy(runs, func(e history.Entry) string { return e.Hostname }, 4)
	cwdBars := topBy(runs, func(e history.Entry) string { return e.Cwd }, 4)
	timeBars := bucketRuns(runs, g)
	timeBars = lastBars(timeBars, 6)

	sections := []struct {
		title string
		bars  []chartBar
	}{
		{"usage by " + g.label(), timeBars},
		{"hostname", hostBars},
		{"directory", cwdBars},
	}
	var chunks []string
	remain := maxH
	for _, sec := range sections {
		if remain < 2 {
			break
		}
		block := renderBarSection(th, sec.title, sec.bars, w, remain)
		h := lipgloss.Height(block)
		if h <= 0 {
			continue
		}
		if h > remain {
			break
		}
		chunks = append(chunks, block)
		remain -= h
	}
	return strings.Join(chunks, "\n")
}

func lastBars(in []chartBar, n int) []chartBar {
	if n <= 0 || len(in) <= n {
		return in
	}
	return in[len(in)-n:]
}

func renderBarSection(th Theme, title string, bars []chartBar, w, maxH int) string {
	if maxH < 2 || len(bars) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(th.Muted.Render(title))
	used := 1
	maxN := 0
	for _, bar := range bars {
		if bar.Total > maxN {
			maxN = bar.Total
		}
	}
	for _, bar := range bars {
		if used >= maxH {
			break
		}
		b.WriteByte('\n')
		b.WriteString(clampLine(renderBarRow(th, bar, maxN, w), w))
		used++
	}
	return b.String()
}

func renderBarRow(th Theme, bar chartBar, maxN, w int) string {
	labelW := 12
	if w < 40 {
		labelW = 8
	}
	count := fmt.Sprintf("%d", bar.Total)
	if bar.Fail > 0 {
		count = fmt.Sprintf("%d/%d", bar.Total-bar.Fail, bar.Fail)
	}
	countS := th.Muted.Render(count)
	label := ansi.Truncate(bar.Label, labelW, "...")
	if lipgloss.Width(label) < labelW {
		label += strings.Repeat(" ", labelW-lipgloss.Width(label))
	}
	labelS := th.Time.Render(label)
	prefix := "  " + labelS + " "
	suffix := " " + countS
	barW := w - lipgloss.Width(prefix) - lipgloss.Width(suffix)
	if barW < 4 {
		barW = 4
	}
	return prefix + renderGlyphBar(th, bar, maxN, barW) + suffix
}

func renderGlyphBar(th Theme, bar chartBar, maxN, width int) string {
	if width <= 0 || maxN <= 0 || bar.Total <= 0 {
		return strings.Repeat(" ", max(0, width))
	}
	n := (bar.Total*width + maxN - 1) / maxN
	if n < 1 {
		n = 1
	}
	if n > width {
		n = width
	}
	ok := bar.Total - bar.Fail
	okN := 0
	if bar.Total > 0 {
		okN = (ok * n) / bar.Total
	}
	failN := n - okN
	if bar.Fail > 0 && failN < 1 && n > 1 {
		failN = 1
		okN = n - 1
	}
	if bar.Fail == 0 {
		failN = 0
		okN = n
	}
	return th.Accent.Render(strings.Repeat("█", okN)) +
		th.Failed.Render(strings.Repeat("█", failN)) +
		strings.Repeat(" ", width-n)
}
