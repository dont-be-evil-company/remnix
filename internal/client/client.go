package client

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync/atomic"
	"time"

	"github.com/dont-be-evil-company/remnix/internal/config"
	"github.com/dont-be-evil-company/remnix/internal/history"
	"github.com/dont-be-evil-company/remnix/internal/protocol"
)

type Client struct {
	conn   net.Conn
	nextID atomic.Uint64
}

func Dial() (*Client, error) {
	conn, err := net.DialTimeout("unix", config.ControlSocketPath(), 80*time.Millisecond)
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn}, nil
}

func Ensure() (*Client, error) {
	if c, err := Dial(); err == nil {
		return c, nil
	}
	if err := startDaemon(); err != nil {
		return nil, err
	}
	var last error
	for i := 0; i < 40; i++ {
		c, err := Dial()
		if err == nil {
			return c, nil
		}
		last = err
		time.Sleep(25 * time.Millisecond)
	}
	return nil, fmt.Errorf("daemon not reachable: %w", last)
}

func startDaemon() error {
	bin, err := os.Executable()
	if err != nil {
		bin = "remnix"
	}
	cmd := exec.Command(bin, "daemon")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) Call(op string, payload any, out any) error {
	return c.call(op, "", payload, out, 5*time.Second)
}

func (c *Client) CallSession(op, sessionID string, payload any, out any) error {
	return c.call(op, sessionID, payload, out, 5*time.Second)
}

func (c *Client) call(op, sessionID string, payload any, out any, timeout time.Duration) error {
	body, err := protocol.MarshalPayload(payload)
	if err != nil {
		return err
	}
	req := protocol.Request{
		Version:   protocol.Version,
		ID:        c.nextID.Add(1),
		Op:        op,
		SessionID: sessionID,
		Payload:   body,
	}
	if timeout > 0 {
		_ = c.conn.SetDeadline(time.Now().Add(timeout))
	} else {
		_ = c.conn.SetDeadline(time.Time{})
	}
	if err := protocol.WriteFrame(c.conn, req); err != nil {
		return err
	}
	var resp protocol.Response
	if err := protocol.ReadFrame(c.conn, &resp); err != nil {
		return err
	}
	if resp.Status != protocol.StatusOK {
		if resp.Error == "" {
			return fmt.Errorf("daemon error")
		}
		return fmt.Errorf("%s", resp.Error)
	}
	if out == nil {
		return nil
	}
	return protocol.UnmarshalPayload(resp.Payload, out)
}

func Suggest(prefix, cwd string) (string, error) {
	c, err := Ensure()
	if err != nil {
		return "", err
	}
	defer c.Close()
	var out string
	if err := c.Call(protocol.OpSuggest, protocol.SuggestReq{Prefix: prefix, Cwd: cwd}, &out); err != nil {
		return "", err
	}
	return out, nil
}

func SuggestList(prefix, cwd string) ([]string, error) {
	c, err := Ensure()
	if err != nil {
		return nil, err
	}
	defer c.Close()
	var out protocol.SuggestListRes
	if err := c.Call(protocol.OpSuggestList, protocol.SuggestListReq{Prefix: prefix, Cwd: cwd}, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func HistoryStart(command, cwd, session, shell string) (string, error) {
	c, err := Ensure()
	if err != nil {
		return "", err
	}
	defer c.Close()
	var out protocol.HistoryStartRes
	if err := c.Call(protocol.OpHistoryStart, protocol.HistoryStartReq{
		Command: command, Cwd: cwd, Session: session, Shell: shell,
	}, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func HistoryEnd(id string, exit int) error {
	c, err := Ensure()
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Call(protocol.OpHistoryEnd, protocol.HistoryEndReq{ID: id, Exit: exit}, nil)
}

func HistorySearch(in protocol.HistorySearchReq) ([]protocol.SearchHit, error) {
	c, err := Ensure()
	if err != nil {
		return nil, err
	}
	defer c.Close()
	var out protocol.HistorySearchRes
	if err := c.Call(protocol.OpHistorySearch, in, &out); err != nil {
		return nil, err
	}
	return out.Hits, nil
}

func HistoryDelete(command string) error {
	c, err := Ensure()
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Call(protocol.OpHistoryDelete, protocol.HistoryDeleteReq{Command: command}, nil)
}

func TombstoneEntries(entries []history.Entry) error {
	c, err := Ensure()
	if err != nil {
		return err
	}
	defer c.Close()
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	return c.Call(protocol.OpHistoryTombEnt, protocol.HistoryTombstoneReq{IDs: ids}, nil)
}

func SyncNow(req protocol.SyncNowReq) error {
	c, err := Ensure()
	if err != nil {
		return err
	}
	defer c.Close()
	return c.call(protocol.OpSyncNow, "", req, nil, 15*time.Minute)
}

func PeekStats() (protocol.Stats, error) {
	c, err := Dial()
	if err != nil {
		return protocol.Stats{}, err
	}
	defer c.Close()
	var st protocol.Stats
	if err := c.Call(protocol.OpDaemonStats, nil, &st); err != nil {
		return protocol.Stats{}, err
	}
	return st, nil
}

func Stats() (protocol.Stats, error) {
	c, err := Ensure()
	if err != nil {
		return protocol.Stats{}, err
	}
	defer c.Close()
	var st protocol.Stats
	if err := c.Call(protocol.OpDaemonStats, nil, &st); err != nil {
		return protocol.Stats{}, err
	}
	return st, nil
}

func ReloadConfig() error {
	c, err := Dial()
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Call(protocol.OpReloadConfig, nil, nil)
}

func CompactCache() (protocol.Stats, error) {
	c, err := Dial()
	if err != nil {
		return protocol.Stats{}, err
	}
	defer c.Close()
	var st protocol.Stats
	if err := c.Call(protocol.OpCompactCache, nil, &st); err != nil {
		return protocol.Stats{}, err
	}
	return st, nil
}

func HitsToEntries(hits []protocol.SearchHit) []history.Entry {
	out := make([]history.Entry, 0, len(hits))
	for _, h := range hits {
		e := history.Entry{
			ID:         h.ID,
			Command:    h.Command,
			Cwd:        h.Cwd,
			DeviceID:   h.DeviceID,
			SessionID:  h.SessionID,
			Hostname:   h.Hostname,
			Shell:      h.Shell,
			StartTS:    time.UnixMilli(h.StartTS).UTC(),
			ExitStatus: h.Exit,
		}
		out = append(out, e)
	}
	return out
}
