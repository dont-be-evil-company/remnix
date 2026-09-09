package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/dont-be-evil-company/remnix/internal/config"
)

func Serve(r io.Reader, w io.Writer, svc *Service) error {
	br := bufio.NewReader(r)
	for {
		if err := serveOne(br, w, svc); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func serveOne(r *bufio.Reader, w io.Writer, svc *Service) error {
	op, err := readField(r)
	if err != nil {
		return err
	}
	switch op {
	case opPing:
		return writeOK(w, "")
	case opSuggest:
		prefix, err := readField(r)
		if err != nil {
			return err
		}
		cwd, err := readField(r)
		if err != nil {
			return err
		}
		s, err := svc.Suggest(prefix, cwd)
		if err != nil {
			return writeErr(w, err.Error())
		}
		return writeOK(w, s)
	case opSuggestList:
		prefix, err := readField(r)
		if err != nil {
			return err
		}
		cwd, err := readField(r)
		if err != nil {
			return err
		}
		items, err := svc.SuggestList(prefix, cwd)
		if err != nil {
			return writeErr(w, err.Error())
		}
		return writeOKList(w, items)
	case opStart:
		command, err := readField(r)
		if err != nil {
			return err
		}
		cwd, err := readField(r)
		if err != nil {
			return err
		}
		session, err := readField(r)
		if err != nil {
			return err
		}
		shell, err := readField(r)
		if err != nil {
			return err
		}
		id, err := svc.Start(command, cwd, session, shell)
		if err != nil {
			return writeErr(w, err.Error())
		}
		return writeOK(w, id)
	case opEnd:
		id, err := readField(r)
		if err != nil {
			return err
		}
		exitRaw, err := readField(r)
		if err != nil {
			return err
		}
		exit, _ := strconv.Atoi(exitRaw)
		if err := svc.End(id, exit); err != nil {
			return writeErr(w, err.Error())
		}
		return writeOK(w, "")
	default:
		return writeErr(w, fmt.Sprintf("unknown op %q", op))
	}
}

func ListenAndServe(ctx context.Context, svc *Service) error {
	if err := Ping(); err == nil {
		return nil
	}
	ln, err := listen()
	if err != nil {
		if err := Ping(); err == nil {
			return nil
		}
		return err
	}
	defer ln.Close()
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go func(c net.Conn) {
			defer c.Close()
			_ = Serve(c, c, svc)
		}(conn)
	}
}

func listen() (net.Listener, error) {
	path := config.AgentSocketPath()
	if err := os.MkdirAll(config.RuntimeDir(), 0o700); err != nil {
		return nil, err
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	return ln, nil
}

func Ping() error {
	conn, err := dial()
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := writeField(conn, opPing); err != nil {
		return err
	}
	_, err = readReply(bufio.NewReader(conn))
	return err
}

func dial() (net.Conn, error) {
	return net.DialTimeout("unix", config.AgentSocketPath(), 50*time.Millisecond)
}

func DialRPC(op string, fields ...string) (string, error) {
	conn, err := dial()
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(200 * time.Millisecond))
	if err := writeField(conn, op); err != nil {
		return "", err
	}
	for _, f := range fields {
		if err := writeField(conn, f); err != nil {
			return "", err
		}
	}
	return readReply(bufio.NewReader(conn))
}

func DialRPCList(op string, fields ...string) ([]string, error) {
	conn, err := dial()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(200 * time.Millisecond))
	if err := writeField(conn, op); err != nil {
		return nil, err
	}
	for _, f := range fields {
		if err := writeField(conn, f); err != nil {
			return nil, err
		}
	}
	return readReplyList(bufio.NewReader(conn))
}
