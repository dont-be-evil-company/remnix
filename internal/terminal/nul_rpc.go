package terminal

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
)

func serveInheritedNUL(f *os.File, rpc RPC) {
	defer f.Close()
	br := bufio.NewReader(f)
	for {
		if err := serveNULOne(br, f, rpc); err != nil {
			return
		}
	}
}

func serveNULOne(r *bufio.Reader, w io.Writer, rpc RPC) error {
	op, err := nulReadField(r)
	if err != nil {
		return err
	}
	switch op {
	case "ping":
		return nulWriteOK(w, "")
	case "suggest":
		prefix, err := nulReadField(r)
		if err != nil {
			return err
		}
		cwd, err := nulReadField(r)
		if err != nil {
			return err
		}
		if rpc.Suggest == nil {
			return nulWriteOK(w, "")
		}
		s, err := rpc.Suggest(prefix, cwd)
		if err != nil {
			return nulWriteErr(w, err.Error())
		}
		return nulWriteOK(w, s)
	case "start":
		command, err := nulReadField(r)
		if err != nil {
			return err
		}
		cwd, err := nulReadField(r)
		if err != nil {
			return err
		}
		session, err := nulReadField(r)
		if err != nil {
			return err
		}
		shell, err := nulReadField(r)
		if err != nil {
			return err
		}
		if rpc.Start == nil {
			return nulWriteOK(w, "")
		}
		id, err := rpc.Start(command, cwd, session, shell)
		if err != nil {
			return nulWriteErr(w, err.Error())
		}
		return nulWriteOK(w, id)
	case "end":
		id, err := nulReadField(r)
		if err != nil {
			return err
		}
		exitRaw, err := nulReadField(r)
		if err != nil {
			return err
		}
		exit, _ := strconv.Atoi(exitRaw)
		if rpc.End != nil {
			if err := rpc.End(id, exit); err != nil {
				return nulWriteErr(w, err.Error())
			}
		}
		return nulWriteOK(w, "")
	default:
		return nulWriteErr(w, fmt.Sprintf("unknown op %q", op))
	}
}

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

func nulWriteErr(w io.Writer, msg string) error {
	if err := nulWriteField(w, "err"); err != nil {
		return err
	}
	return nulWriteField(w, msg)
}
