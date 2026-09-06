package daemon

import (
	"bufio"
	"context"
	"io"
	"net"
	"time"

	"github.com/mistweaverco/syncsh/internal/history"
	"github.com/mistweaverco/syncsh/internal/protocol"
	"github.com/mistweaverco/syncsh/internal/search"
)

const pingTimeout = 50 * time.Millisecond

func (s *Server) serveControl(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		_ = s.controlLn.Close()
	}()
	for {
		conn, err := s.controlLn.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if !peerAllowed(conn) {
			_ = conn.Close()
			continue
		}
		go func(c net.Conn) {
			defer func() { _ = recover() }()
			defer c.Close()
			_ = s.handleControl(c)
		}(conn)
	}
}

func (s *Server) serveTerminal(ctx context.Context) {
	go func() {
		<-ctx.Done()
		if s.termLn != nil {
			_ = s.termLn.Close()
		}
	}()
	if s.termLn == nil {
		return
	}
	for {
		conn, err := s.termLn.Accept()
		if err != nil {
			return
		}
		if !peerAllowed(conn) {
			_ = conn.Close()
			continue
		}
		go func(c net.Conn) {
			defer func() { _ = recover() }()
			if s.sessions != nil {
				_ = s.sessions.Accept(c)
			} else {
				_ = c.Close()
			}
		}(conn)
	}
}

func (s *Server) handleControl(conn net.Conn) error {
	br := bufio.NewReader(conn)
	first, err := br.Peek(1)
	if err != nil {
		return err
	}
	if first[0] != 0 {
		return s.serveNUL(br, conn)
	}
	for {
		var req protocol.Request
		if err := protocol.ReadFrame(br, &req); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		resp := s.dispatch(req)
		if err := protocol.WriteFrame(conn, resp); err != nil {
			return err
		}
	}
}

func pingConn(conn net.Conn) error {
	_ = conn.SetDeadline(time.Now().Add(200 * time.Millisecond))
	req := protocol.Request{Version: protocol.Version, ID: 1, Op: protocol.OpPing}
	if err := protocol.WriteFrame(conn, req); err != nil {
		return err
	}
	var resp protocol.Response
	if err := protocol.ReadFrame(conn, &resp); err != nil {
		return err
	}
	if resp.Status != protocol.StatusOK {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func (s *Server) dispatch(req protocol.Request) protocol.Response {
	if req.Version != 0 && req.Version != protocol.Version {
		return protocol.EncodeErr(req.ID, "unsupported protocol version")
	}
	switch req.Op {
	case protocol.OpPing:
		resp, _ := protocol.EncodeOK(req.ID, nil)
		return resp
	case protocol.OpVersion:
		resp, err := protocol.EncodeOK(req.ID, s.Version())
		if err != nil {
			return protocol.EncodeErr(req.ID, err.Error())
		}
		return resp
	case protocol.OpCapabilities:
		resp, err := protocol.EncodeOK(req.ID, s.Capabilities())
		if err != nil {
			return protocol.EncodeErr(req.ID, err.Error())
		}
		return resp
	case protocol.OpSuggest:
		var in protocol.SuggestReq
		_ = protocol.UnmarshalPayload(req.Payload, &in)
		cands, err := s.history.SuggestCandidates(in.Prefix, in.Cwd)
		if err != nil {
			return protocol.EncodeErr(req.ID, err.Error())
		}
		sugg := search.BestSuggestion(in.Prefix, cands, search.Context{Cwd: in.Cwd, DeviceID: s.deviceID()})
		resp, err := protocol.EncodeOK(req.ID, sugg)
		if err != nil {
			return protocol.EncodeErr(req.ID, err.Error())
		}
		return resp
	case protocol.OpSuggestList:
		var in protocol.SuggestListReq
		_ = protocol.UnmarshalPayload(req.Payload, &in)
		limit := in.Limit
		if limit <= 0 && s.app != nil {
			limit = s.app.Config.Suggest.MenuLimit()
		}
		cands, err := s.history.SuggestCandidates(in.Prefix, in.Cwd)
		if err != nil {
			return protocol.EncodeErr(req.ID, err.Error())
		}
		items := search.Suggestions(in.Prefix, cands, search.Context{Cwd: in.Cwd, DeviceID: s.deviceID()}, limit)
		resp, err := protocol.EncodeOK(req.ID, protocol.SuggestListRes{Items: items})
		if err != nil {
			return protocol.EncodeErr(req.ID, err.Error())
		}
		return resp
	case protocol.OpHistoryStart:
		var in protocol.HistoryStartReq
		_ = protocol.UnmarshalPayload(req.Payload, &in)
		id, err := s.history.StartCommand(in.Command, in.Cwd, in.Session, in.Shell)
		if err != nil {
			return protocol.EncodeErr(req.ID, err.Error())
		}
		resp, err := protocol.EncodeOK(req.ID, protocol.HistoryStartRes{ID: id})
		if err != nil {
			return protocol.EncodeErr(req.ID, err.Error())
		}
		return resp
	case protocol.OpHistoryEnd:
		var in protocol.HistoryEndReq
		_ = protocol.UnmarshalPayload(req.Payload, &in)
		if err := s.history.CompleteCommand(in.ID, in.Exit); err != nil {
			return protocol.EncodeErr(req.ID, err.Error())
		}
		resp, _ := protocol.EncodeOK(req.ID, nil)
		return resp
	case protocol.OpHistorySearch:
		var in protocol.HistorySearchReq
		_ = protocol.UnmarshalPayload(req.Payload, &in)
		cands, err := s.history.SearchCandidates(in.Query, in.Cwd, in.SessionID, in.Exact, in.Limit)
		if err != nil {
			return protocol.EncodeErr(req.ID, err.Error())
		}
		ranked := search.RankWith(in.Query, cands, in.Exact, search.Context{
			Cwd: in.Cwd, DeviceID: s.deviceID(), SessionID: in.SessionID,
		})
		limit := in.Limit
		if limit <= 0 || limit > len(ranked) {
			limit = len(ranked)
		}
		entries := make([]history.Entry, 0, limit)
		for _, r := range ranked[:limit] {
			entries = append(entries, r.Entry)
		}
		hits := make([]protocol.SearchHit, 0, len(entries))
		for _, e := range entries {
			hits = append(hits, protocol.SearchHit{
				ID:        e.ID,
				Command:   e.Command,
				Cwd:       e.Cwd,
				DeviceID:  e.DeviceID,
				SessionID: e.SessionID,
				Hostname:  e.Hostname,
				Shell:     e.Shell,
				StartTS:   e.StartTS.UnixMilli(),
				Exit:      e.ExitStatus,
			})
		}
		resp, err := protocol.EncodeOK(req.ID, protocol.HistorySearchRes{Hits: hits})
		if err != nil {
			return protocol.EncodeErr(req.ID, err.Error())
		}
		return resp
	case protocol.OpHistoryDelete:
		var in protocol.HistoryDeleteReq
		_ = protocol.UnmarshalPayload(req.Payload, &in)
		if err := s.history.TombstoneCommand(in.Command); err != nil {
			return protocol.EncodeErr(req.ID, err.Error())
		}
		resp, _ := protocol.EncodeOK(req.ID, nil)
		return resp
	case protocol.OpHistoryTombEnt:
		var in protocol.HistoryTombstoneReq
		_ = protocol.UnmarshalPayload(req.Payload, &in)
		entries := make([]history.Entry, 0, len(in.IDs))
		for _, id := range in.IDs {
			entries = append(entries, history.Entry{ID: id})
		}
		if err := s.history.TombstoneEntries(entries); err != nil {
			return protocol.EncodeErr(req.ID, err.Error())
		}
		resp, _ := protocol.EncodeOK(req.ID, nil)
		return resp
	case protocol.OpSyncNow:
		class := s.syncer.SyncNow(context.Background())
		if class == "busy" {
			return protocol.EncodeErr(req.ID, "sync already running")
		}
		resp, _ := protocol.EncodeOK(req.ID, class)
		return resp
	case protocol.OpDaemonStats:
		resp, err := protocol.EncodeOK(req.ID, s.Stats())
		if err != nil {
			return protocol.EncodeErr(req.ID, err.Error())
		}
		return resp
	case protocol.OpDaemonStatus:
		resp, err := protocol.EncodeOK(req.ID, s.lastSyncCopy())
		if err != nil {
			return protocol.EncodeErr(req.ID, err.Error())
		}
		return resp
	case protocol.OpReloadConfig:
		if err := s.ReloadConfig(); err != nil {
			return protocol.EncodeErr(req.ID, err.Error())
		}
		resp, _ := protocol.EncodeOK(req.ID, nil)
		return resp
	case protocol.OpScreenSnapshot:
		if s.sessions == nil {
			return protocol.EncodeErr(req.ID, "no terminal sessions")
		}
		snap, err := s.sessions.Snapshot(req.SessionID)
		if err != nil {
			return protocol.EncodeErr(req.ID, err.Error())
		}
		resp, err := protocol.EncodeOK(req.ID, snap)
		if err != nil {
			return protocol.EncodeErr(req.ID, err.Error())
		}
		return resp
	default:
		return protocol.EncodeErr(req.ID, "unknown op "+req.Op)
	}
}

func (s *Server) lastSyncCopy() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSync
}
