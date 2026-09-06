package daemon

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
)

func (s *Server) serveNUL(br *bufio.Reader, w io.Writer) error {
	for {
		if err := s.serveNULOne(br, w); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func (s *Server) serveNULOne(r *bufio.Reader, w io.Writer) error {
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
		cands, err := s.history.SuggestCandidates(prefix, cwd)
		if err != nil {
			return nulWriteErr(w, err.Error())
		}
		return nulWriteOK(w, s.bestSuggestion(prefix, cwd, cands))
	case "suggest-list":
		prefix, err := nulReadField(r)
		if err != nil {
			return err
		}
		cwd, err := nulReadField(r)
		if err != nil {
			return err
		}
		limit := 8
		if s.app != nil {
			limit = s.app.Config.Suggest.MenuLimit()
		}
		cands, err := s.history.SuggestCandidates(prefix, cwd)
		if err != nil {
			return nulWriteErr(w, err.Error())
		}
		items := s.suggestList(prefix, cwd, cands, limit)
		return nulWriteOKList(w, items)
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
		id, err := s.history.StartCommand(command, cwd, session, shell)
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
		if err := s.history.CompleteCommand(id, exit); err != nil {
			return nulWriteErr(w, err.Error())
		}
		return nulWriteOK(w, "")
	case "search-interactive":
		query, err := nulReadField(r)
		if err != nil {
			return err
		}
		cwd, err := nulReadField(r)
		if err != nil {
			return err
		}
		sessionID, err := nulReadField(r)
		if err != nil {
			return err
		}
		sel, err := s.searchInteractive(query, cwd, sessionID)
		if err != nil {
			return nulWriteErr(w, err.Error())
		}
		return nulWriteOK(w, sel)
	case "suggest-interactive":
		prefix, err := nulReadField(r)
		if err != nil {
			return err
		}
		cwd, err := nulReadField(r)
		if err != nil {
			return err
		}
		sessionID, err := nulReadField(r)
		if err != nil {
			return err
		}
		sel, err := s.suggestInteractive(prefix, cwd, sessionID)
		if err != nil {
			return nulWriteErr(w, err.Error())
		}
		return nulWriteOK(w, sel)
	default:
		return nulWriteErr(w, fmt.Sprintf("unknown op %q", op))
	}
}
