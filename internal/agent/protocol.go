package agent

import (
	"bufio"
	"fmt"
	"io"
)

const (
	opPing        = "ping"
	opSuggest     = "suggest"
	opSuggestList = "suggest-list"
	opStart       = "start"
	opEnd         = "end"
	statusOK      = "ok"
	statusErr     = "err"
)

func readField(r *bufio.Reader) (string, error) {
	b, err := r.ReadBytes(0)
	if err != nil {
		return "", err
	}
	if len(b) == 0 {
		return "", io.ErrUnexpectedEOF
	}
	return string(b[:len(b)-1]), nil
}

func writeField(w io.Writer, s string) error {
	if _, err := io.WriteString(w, s); err != nil {
		return err
	}
	_, err := w.Write([]byte{0})
	return err
}

func writeOK(w io.Writer, payload string) error {
	if err := writeField(w, statusOK); err != nil {
		return err
	}
	return writeField(w, payload)
}

func writeOKList(w io.Writer, items []string) error {
	if err := writeField(w, statusOK); err != nil {
		return err
	}
	if err := writeField(w, fmt.Sprintf("%d", len(items))); err != nil {
		return err
	}
	for _, s := range items {
		if err := writeField(w, s); err != nil {
			return err
		}
	}
	return nil
}

func writeErr(w io.Writer, msg string) error {
	if err := writeField(w, statusErr); err != nil {
		return err
	}
	return writeField(w, msg)
}

func readReply(r *bufio.Reader) (payload string, err error) {
	st, err := readField(r)
	if err != nil {
		return "", err
	}
	payload, err = readField(r)
	if err != nil {
		return "", err
	}
	if st != statusOK {
		if payload == "" {
			payload = "agent error"
		}
		return "", fmt.Errorf("%s", payload)
	}
	return payload, nil
}

func readReplyList(r *bufio.Reader) ([]string, error) {
	st, err := readField(r)
	if err != nil {
		return nil, err
	}
	nRaw, err := readField(r)
	if err != nil {
		return nil, err
	}
	if st != statusOK {
		if nRaw == "" {
			nRaw = "agent error"
		}
		return nil, fmt.Errorf("%s", nRaw)
	}
	n := 0
	if nRaw != "" {
		_, _ = fmt.Sscanf(nRaw, "%d", &n)
	}
	if n < 0 {
		n = 0
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		s, err := readField(r)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}
