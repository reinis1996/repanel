package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// tlsErrorFilter drops the per-connection "TLS handshake error" lines that Go's
// http server logs for benign client behaviour — port scanners and clients that
// reject the panel's self-signed certificate. Genuine certificate problems
// surface at startup (when the cert is loaded), not here, so this only removes
// noise; every other server log line is passed through unchanged.
type tlsErrorFilter struct{ w io.Writer }

func (f tlsErrorFilter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("TLS handshake error")) {
		return len(p), nil
	}
	return f.w.Write(p)
}

// httpsOnlyListener wraps a TCP listener and classifies each connection by its
// first byte. A TLS connection (handshake record type 0x16) is handed to the TLS
// server unchanged; a plain HTTP request sent to the HTTPS port is answered with
// a redirect to the same URL over https — instead of failing the TLS handshake
// with the confusing "client sent an HTTP request to an HTTPS server" error and
// leaving the user with a blank page.
//
// Classification (the first-byte peek) runs in a per-connection goroutine and
// only finished TLS connections are delivered to Accept, so a slow or silent
// client cannot stall the accept loop and starve new connections.
type httpsOnlyListener struct {
	net.Listener
	once  sync.Once
	ready chan net.Conn
	mu    sync.Mutex
	err   error
}

func (l *httpsOnlyListener) Accept() (net.Conn, error) {
	l.once.Do(func() {
		l.ready = make(chan net.Conn)
		go l.acceptLoop()
	})
	c, ok := <-l.ready
	if !ok {
		l.mu.Lock()
		defer l.mu.Unlock()
		return nil, l.err
	}
	return c, nil
}

func (l *httpsOnlyListener) acceptLoop() {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			l.mu.Lock()
			l.err = err
			l.mu.Unlock()
			close(l.ready)
			return
		}
		go l.classify(c)
	}
}

func (l *httpsOnlyListener) classify(c net.Conn) {
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	b := make([]byte, 1)
	n, err := c.Read(b)
	_ = c.SetReadDeadline(time.Time{})
	if err != nil || n == 0 {
		c.Close()
		return
	}
	pc := &prefixConn{Conn: c, prefix: b[:n]}
	if b[0] != 0x16 { // not a TLS handshake — treat as plain HTTP
		redirectToHTTPS(pc)
		return
	}
	// Deliver the TLS connection to Accept. ready may be closed if the listener
	// shut down between the Accept and here; recover and drop the connection.
	defer func() {
		if recover() != nil {
			pc.Close()
		}
	}()
	l.ready <- pc
}

// prefixConn re-prepends bytes already read from the connection so the eventual
// consumer (the TLS handshake or the HTTP parser) still sees the full stream.
type prefixConn struct {
	net.Conn
	prefix []byte
}

func (c *prefixConn) Read(p []byte) (int, error) {
	if len(c.prefix) > 0 {
		n := copy(p, c.prefix)
		c.prefix = c.prefix[n:]
		return n, nil
	}
	return c.Conn.Read(p)
}

// redirectToHTTPS answers a plain-HTTP request with a redirect to the same URL
// over https, so someone who opened http://<panel> lands on the HTTPS panel.
func redirectToHTTPS(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	req, err := http.ReadRequest(bufio.NewReader(c))
	if err != nil || req.Host == "" {
		const body = "RePanel is served over HTTPS only — open this address with https://"
		fmt.Fprintf(c, "HTTP/1.1 426 Upgrade Required\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
		return
	}
	loc := "https://" + req.Host + req.URL.RequestURI()
	body := "RePanel requires HTTPS. Redirecting to " + loc + "\n"
	fmt.Fprintf(c, "HTTP/1.1 308 Permanent Redirect\r\nLocation: %s\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", loc, len(body), body)
}
