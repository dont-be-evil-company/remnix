// tui_child is a generic high-output interactive fixture. It records every
// stdin byte and optionally emits sustained ANSI, without depending on any
// real TUI application.
package main

import (
	"encoding/binary"
	"io"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

func main() {
	recordPath := os.Getenv("REMNIX_PTY_RECORD")
	if recordPath == "" {
		os.Exit(2)
	}
	expect, _ := strconv.Atoi(os.Getenv("REMNIX_PTY_EXPECT"))
	mode := os.Getenv("REMNIX_PTY_OUTPUT")
	chunk, _ := strconv.Atoi(os.Getenv("REMNIX_PTY_CHUNK"))
	if chunk < 1 {
		chunk = 512
	}
	seed, _ := strconv.ParseInt(os.Getenv("REMNIX_PTY_SEED"), 10, 64)
	yield := os.Getenv("REMNIX_PTY_YIELD") == "1"

	_ = unix.SetNonblock(0, false)
	if err := forceRaw(0); err != nil {
		_ = os.WriteFile(recordPath+".err", []byte(err.Error()), 0o644)
	}

	f, err := os.OpenFile(recordPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		os.Exit(4)
	}
	defer f.Close()

	_, _ = os.Stdout.Write([]byte("READY\n"))
	_ = os.Stdout.Sync()

	var stop atomic.Bool
	if mode != "" && mode != "none" {
		go emitOutput(mode, chunk, seed, yield, &stop)
	}

	buf := make([]byte, 4096)
	got := 0
	for {
		n, err := unix.Read(0, buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				stop.Store(true)
				os.Exit(5)
			}
			got += n
			if expect > 0 && got >= expect {
				_ = f.Sync()
				stop.Store(true)
				os.Stdout.Write(endMarker(uint64(got)))
				os.Exit(0)
			}
		}
		if err == unix.EINTR || err == unix.EAGAIN || err == unix.EWOULDBLOCK {
			continue
		}
		if n == 0 || err == io.EOF {
			if expect > 0 && got < expect {
				time.Sleep(time.Millisecond)
				continue
			}
			_ = f.Sync()
			stop.Store(true)
			os.Exit(0)
		}
		if err != nil {
			_ = f.Sync()
			stop.Store(true)
			os.Exit(6)
		}
	}
}

func forceRaw(fd int) error {
	_, err := term.MakeRaw(fd)
	return err
}

func endMarker(n uint64) []byte {
	var b [12]byte
	copy(b[:4], "END:")
	binary.BigEndian.PutUint64(b[4:], n)
	return b[:]
}

func emitOutput(mode string, chunk int, seed int64, yield bool, stop *atomic.Bool) {
	rng := seed
	if rng == 0 {
		rng = 1
	}
	next := func() int64 {
		rng = rng*1664525 + 1013904223
		if rng < 0 {
			rng = -rng
		}
		return rng
	}
	csi := []byte("\x1b[H\x1b[2J\x1b[38;5;11m")
	line := make([]byte, chunk)
	for i := range line {
		line[i] = byte('A' + i%26)
	}
	line[len(line)-1] = '\n'
	burst := 1
	switch mode {
	case "moderate":
		burst = 4
	case "heavy":
		burst = 64
	case "burst":
		burst = 256
	}
	for !stop.Load() {
		if mode == "burst" && next()%7 == 0 {
			for i := 0; i < burst && !stop.Load(); i++ {
				_, _ = os.Stdout.Write(csi)
				_, _ = os.Stdout.Write(line)
			}
		} else {
			_, _ = os.Stdout.Write(csi)
			n := 8 + int(next()%int64(len(line)-8))
			if n > len(line) {
				n = len(line)
			}
			_, _ = os.Stdout.Write(line[:n])
		}
		if yield {
			time.Sleep(time.Duration(next()%3) * time.Millisecond)
		} else if mode == "moderate" {
			time.Sleep(time.Millisecond)
		}
	}
}
