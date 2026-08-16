package importers

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mistweaverco/syncsh/internal/history"
)

type Record struct {
	Command  string
	StartTS  time.Time
	Duration *int64
	Exit     *int
	Cwd      string
}

func ParseHistfile(r io.Reader, kind string) ([]Record, error) {
	switch strings.ToLower(kind) {
	case "zsh":
		return parseZsh(r)
	case "bash":
		return parseBash(r)
	case "fish":
		return parseFish(r)
	default:
		return nil, fmt.Errorf("unknown histfile kind %q", kind)
	}
}

func DetectKind(path string, firstLine string) string {
	base := strings.ToLower(path)
	switch {
	case strings.Contains(base, "fish"):
		return "fish"
	case strings.Contains(base, "zsh"):
		return "zsh"
	case strings.Contains(base, "bash"):
		return "bash"
	}
	if strings.HasPrefix(strings.TrimSpace(firstLine), ": ") {
		return "zsh"
	}
	if strings.HasPrefix(strings.TrimSpace(firstLine), "- cmd:") {
		return "fish"
	}
	return "bash"
}

func parseZsh(r io.Reader) ([]Record, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var out []Record
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, ": ") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			out = append(out, Record{Command: line, StartTS: time.Unix(0, 0).UTC()})
			continue
		}
		// : timestamp:duration;command
		rest := strings.TrimPrefix(line, ": ")
		semi := strings.IndexByte(rest, ';')
		if semi < 0 {
			continue
		}
		meta, cmd := rest[:semi], rest[semi+1:]
		tsStr, durStr, _ := strings.Cut(meta, ":")
		ts, _ := strconv.ParseInt(strings.TrimSpace(tsStr), 10, 64)
		rec := Record{Command: cmd, StartTS: time.Unix(ts, 0).UTC()}
		if dur, err := strconv.ParseInt(strings.TrimSpace(durStr), 10, 64); err == nil {
			ms := dur * 1000
			rec.Duration = &ms
		}
		out = append(out, rec)
	}
	return out, sc.Err()
}

func parseBash(r io.Reader) ([]Record, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var out []Record
	var pendingTS *time.Time
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") {
			if ts, err := strconv.ParseInt(strings.TrimPrefix(line, "#"), 10, 64); err == nil {
				t := time.Unix(ts, 0).UTC()
				pendingTS = &t
				continue
			}
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		rec := Record{Command: line}
		if pendingTS != nil {
			rec.StartTS = *pendingTS
			pendingTS = nil
		}
		out = append(out, rec)
	}
	return out, sc.Err()
}

func parseFish(r io.Reader) ([]Record, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var out []Record
	var cur *Record
	flush := func() {
		if cur != nil && cur.Command != "" {
			out = append(out, *cur)
		}
		cur = nil
	}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "- cmd:"):
			flush()
			cmd := strings.TrimSpace(strings.TrimPrefix(line, "- cmd:"))
			cmd = strings.Trim(cmd, `"`)
			cur = &Record{Command: cmd}
		case strings.HasPrefix(line, "when:") && cur != nil:
			n, _ := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "when:")), 10, 64)
			cur.StartTS = time.Unix(n, 0).UTC()
		case strings.HasPrefix(line, "paths:") && cur != nil:
			// next line may contain the path; handled loosely
		case strings.HasPrefix(line, "-") && cur != nil && cur.Cwd == "":
			p := strings.TrimSpace(strings.TrimPrefix(line, "-"))
			cur.Cwd = strings.Trim(p, `"`)
		}
	}
	flush()
	return out, sc.Err()
}

func ReadHistfile(path, kind string) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if kind == "" || kind == "auto" {
		br := bufio.NewReader(f)
		first, _ := br.Peek(128)
		kind = DetectKind(path, string(first))
		records, err := ParseHistfile(io.MultiReader(br, f), kind)
		return records, err
	}
	return ParseHistfile(f, kind)
}

func ToEntries(recs []Record, deviceID, shell string) []history.Entry {
	out := make([]history.Entry, 0, len(recs))
	for i, r := range recs {
		if strings.TrimSpace(r.Command) == "" {
			continue
		}
		e := history.Entry{
			Command:    r.Command,
			StartTS:    r.StartTS,
			Cwd:        r.Cwd,
			DeviceID:   deviceID,
			Shell:      shell,
			DurationMs: r.Duration,
			ExitStatus: r.Exit,
		}
		if e.StartTS.IsZero() {
			e.StartTS = time.Unix(int64(i), 0).UTC()
		}
		out = append(out, e)
	}
	return out
}
