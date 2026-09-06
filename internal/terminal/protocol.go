package terminal

import (
	"encoding/binary"
	"fmt"
	"io"
	"strings"
)

const (
	FrameData  byte = 0
	FrameWinch byte = 1
	FrameExit  byte = 2
)

type CreateRequest struct {
	Shell string
	Cwd   string
	Cols  int
	Rows  int
	Env   []string
}

func WriteCreate(w io.Writer, req CreateRequest) error {
	var b strings.Builder
	b.WriteString("CREATE\n")
	fmt.Fprintf(&b, "shell=%s\n", req.Shell)
	fmt.Fprintf(&b, "cwd=%s\n", req.Cwd)
	fmt.Fprintf(&b, "cols=%d\n", req.Cols)
	fmt.Fprintf(&b, "rows=%d\n", req.Rows)
	fmt.Fprintf(&b, "env_count=%d\n", len(req.Env))
	for _, e := range req.Env {
		b.WriteString(e)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	_, err := io.WriteString(w, b.String())
	return err
}

func ReadCreate(r io.Reader) (CreateRequest, error) {
	br := newLineReader(r)
	line, err := br.ReadLine()
	if err != nil {
		return CreateRequest{}, err
	}
	if line != "CREATE" {
		return CreateRequest{}, fmt.Errorf("terminal: want CREATE, got %q", line)
	}
	var req CreateRequest
	envN := 0
	for {
		line, err = br.ReadLine()
		if err != nil {
			return CreateRequest{}, err
		}
		if line == "" {
			break
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "shell":
			req.Shell = v
		case "cwd":
			req.Cwd = v
		case "cols":
			fmt.Sscanf(v, "%d", &req.Cols)
		case "rows":
			fmt.Sscanf(v, "%d", &req.Rows)
		case "env_count":
			fmt.Sscanf(v, "%d", &envN)
			for i := 0; i < envN; i++ {
				el, err := br.ReadLine()
				if err != nil {
					return CreateRequest{}, err
				}
				req.Env = append(req.Env, el)
			}
		}
	}
	return req, nil
}

type lineReader struct {
	r io.Reader
	b []byte
}

func newLineReader(r io.Reader) *lineReader {
	return &lineReader{r: r, b: make([]byte, 1)}
}

func (l *lineReader) ReadLine() (string, error) {
	var out []byte
	for {
		n, err := l.r.Read(l.b)
		if n > 0 {
			if l.b[0] == '\n' {
				return string(out), nil
			}
			out = append(out, l.b[0])
			if len(out) > 1<<20 {
				return "", fmt.Errorf("terminal: header line too long")
			}
			continue
		}
		if err != nil {
			if len(out) > 0 && err == io.EOF {
				return string(out), nil
			}
			return "", err
		}
	}
}

func WriteFrame(w io.Writer, kind byte, payload []byte) error {
	var hdr [5]byte
	hdr[0] = kind
	binary.BigEndian.PutUint32(hdr[1:], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	_, err := w.Write(payload)
	return err
}

func ReadFrame(r io.Reader) (byte, []byte, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(hdr[1:])
	if n > 4<<20 {
		return 0, nil, fmt.Errorf("terminal: frame too large")
	}
	if n == 0 {
		return hdr[0], nil, nil
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return 0, nil, err
	}
	return hdr[0], body, nil
}
