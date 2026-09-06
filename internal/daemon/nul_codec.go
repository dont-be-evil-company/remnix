package daemon

import (
	"bufio"
	"fmt"
	"io"
)

func nulReadField(r *bufio.Reader) (string, error) {
	b, err := r.ReadBytes(0)
	if err != nil {
		return "", err
	}
	if len(b) == 0 {
		return "", io.ErrUnexpectedEOF
	}
	return string(b[:len(b)-1]), nil
}

func nulWriteField(w io.Writer, s string) error {
	if _, err := io.WriteString(w, s); err != nil {
		return err
	}
	_, err := w.Write([]byte{0})
	return err
}

func nulWriteOK(w io.Writer, payload string) error {
	if err := nulWriteField(w, "ok"); err != nil {
		return err
	}
	return nulWriteField(w, payload)
}

func nulWriteOKList(w io.Writer, items []string) error {
	if err := nulWriteField(w, "ok"); err != nil {
		return err
	}
	if err := nulWriteField(w, fmt.Sprintf("%d", len(items))); err != nil {
		return err
	}
	for _, s := range items {
		if err := nulWriteField(w, s); err != nil {
			return err
		}
	}
	return nil
}

func nulWriteErr(w io.Writer, msg string) error {
	if err := nulWriteField(w, "err"); err != nil {
		return err
	}
	return nulWriteField(w, msg)
}
