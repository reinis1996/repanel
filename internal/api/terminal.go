package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"

	"github.com/reinis1996/repanel/internal/models"
)

// Web terminal: an xterm.js front end talks to a PTY-backed shell over a
// WebSocket. Admin only — this is a full shell on the host (the panel runs as
// root), so every session is recorded in the audit log. Messages from the
// client are tagged with a one-byte type ('0' = keystrokes, '1' = resize JSON);
// shell output is sent back as raw binary frames.

var terminalUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     sameOriginWS,
}

// sameOriginWS rejects cross-origin WebSocket upgrades. The session cookie alone
// isn't enough — WebSocket isn't subject to the same-origin policy — so we also
// require the Origin host to match the request host (SameSite=Strict on the
// cookie is a second layer).
func sameOriginWS(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

func (s *Server) handleTerminal(w http.ResponseWriter, r *http.Request, u *models.User) {
	conn, err := terminalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade has already written an error response
	}
	defer conn.Close()
	s.audit(u.ID, u.Username, "terminal.open", "", clientIP(r))

	shell := "/bin/bash"
	if _, err := os.Stat(shell); err != nil {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell, "-l")
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "HOME=/root")
	cmd.Dir = "/root"
	ptmx, err := pty.Start(cmd)
	if err != nil {
		conn.WriteMessage(websocket.BinaryMessage, []byte("failed to start shell: "+err.Error()+"\r\n"))
		return
	}
	defer func() {
		ptmx.Close()
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		cmd.Wait()
	}()

	done := make(chan struct{})
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(func() { close(done) }) }

	// Serialize all writes to the connection (gorilla allows only one writer).
	var writeMu sync.Mutex
	write := func(mt int, b []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteMessage(mt, b)
	}

	// Shell output -> client.
	go func() {
		defer stop()
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				if write(websocket.BinaryMessage, buf[:n]) != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// Keepalive: ping the client so a dead connection is detected and torn down.
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if write(websocket.PingMessage, nil) != nil {
					stop()
					return
				}
			}
		}
	}()

	// Client -> shell (input and resize).
	go func() {
		defer stop()
		conn.SetReadLimit(1 << 20)
		conn.SetReadDeadline(time.Now().Add(75 * time.Second))
		conn.SetPongHandler(func(string) error {
			conn.SetReadDeadline(time.Now().Add(75 * time.Second))
			return nil
		})
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			conn.SetReadDeadline(time.Now().Add(75 * time.Second))
			if len(msg) == 0 {
				continue
			}
			switch msg[0] {
			case '0': // keystrokes
				if _, err := ptmx.Write(msg[1:]); err != nil {
					return
				}
			case '1': // resize: {"cols":N,"rows":M}
				var rs struct {
					Cols uint16 `json:"cols"`
					Rows uint16 `json:"rows"`
				}
				if json.Unmarshal(msg[1:], &rs) == nil && rs.Cols > 0 && rs.Rows > 0 {
					pty.Setsize(ptmx, &pty.Winsize{Cols: rs.Cols, Rows: rs.Rows})
				}
			}
		}
	}()

	<-done
}
