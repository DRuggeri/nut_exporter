package main

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DRuggeri/nut_exporter/v3/collectors"
)

// fakeNUT is a scripted read-only NUT server for handler tests. In normal mode
// it answers a minimal protocol for one UPS exposing ups.load and
// battery.charge. In hang mode it completes the VER/NETVER handshake and then
// stops replying, reporting the moment the client closes the connection.
type fakeNUT struct {
	listener net.Listener
	hang     bool
	closed   chan struct{}
}

var fakeNUTReplies = map[string]string{
	"VER":                          "2.8.0",
	"NETVER":                       "2.8.0",
	"LIST UPS":                     "BEGIN LIST UPS\nUPS demo \"Demo UPS\"\nEND LIST UPS",
	"LIST CLIENT demo":             "BEGIN LIST CLIENT demo\nEND LIST CLIENT demo",
	"LIST CMD demo":                "BEGIN LIST CMD demo\nEND LIST CMD demo",
	"GET UPSDESC demo":             "UPSDESC demo \"Demo UPS\"",
	"GET NUMLOGINS demo":           "NUMLOGINS demo 1",
	"LIST VAR demo":                "BEGIN LIST VAR demo\nVAR demo ups.load \"42\"\nVAR demo battery.charge \"100\"\nEND LIST VAR demo",
	"GET DESC demo ups.load":       "DESC demo ups.load \"Output load\"",
	"GET TYPE demo ups.load":       "TYPE demo ups.load INTEGER",
	"GET DESC demo battery.charge": "DESC demo battery.charge \"Battery charge\"",
	"GET TYPE demo battery.charge": "TYPE demo battery.charge INTEGER",
	"LOGOUT":                       "OK Goodbye",
}

func startFakeNUT(t *testing.T, hang bool) *fakeNUT {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeNUT{listener: listener, hang: hang, closed: make(chan struct{}, 16)}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	t.Cleanup(func() { listener.Close() })
	return f
}

func (f *fakeNUT) port() int {
	return f.listener.Addr().(*net.TCPAddr).Port
}

func (f *fakeNUT) serve(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	if f.hang {
		// Complete the handshake, then swallow everything without replying
		// until the client goes away.
		for i := 0; i < 2; i++ {
			if _, err := reader.ReadString('\n'); err != nil {
				return
			}
			if _, err := io.WriteString(conn, "2.8.0\n"); err != nil {
				return
			}
		}
		_, _ = io.Copy(io.Discard, reader)
		f.closed <- struct{}{}
		return
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		command := strings.TrimSuffix(line, "\n")
		reply, ok := fakeNUTReplies[command]
		if !ok {
			reply = "ERR UNKNOWN-COMMAND"
		}
		if _, err := io.WriteString(conn, reply+"\n"); err != nil {
			return
		}
	}
}

func baseOpts(port int) collectors.NutCollectorOpts {
	return collectors.NutCollectorOpts{
		Namespace:  "network_ups_tools",
		Server:     "127.0.0.1",
		ServerPort: port,
	}
}

func scrape(t *testing.T, h *metricsHandler, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/ups_metrics"+query, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestScrapeHonorsBudgetAndFailsClosed(t *testing.T) {
	fake := startFakeNUT(t, true)
	handler := newMetricsHandler(baseOpts(fake.port()), 300*time.Millisecond)

	start := time.Now()
	rec := scrape(t, handler, "")
	elapsed := time.Since(start)

	if elapsed > 1500*time.Millisecond {
		t.Fatalf("scrape exceeded its budget: took %v, want <1.5s", elapsed)
	}
	if rec.Code == http.StatusOK {
		t.Fatalf("expired scrape returned 200 with body:\n%s", rec.Body.String())
	}
}

func TestScrapeConfigIsPerRequest(t *testing.T) {
	fake := startFakeNUT(t, false)
	handler := newMetricsHandler(baseOpts(fake.port()), 10*time.Second)

	rec := scrape(t, handler, "?variables=ups.load")
	if rec.Code != http.StatusOK {
		t.Fatalf("first scrape: status %d, body:\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "network_ups_tools_ups_load") {
		t.Fatalf("first scrape misses ups.load:\n%s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "network_ups_tools_battery_charge") {
		t.Fatalf("first scrape exported filtered-out battery.charge:\n%s", rec.Body.String())
	}

	rec = scrape(t, handler, "?variables=battery.charge")
	if rec.Code != http.StatusOK {
		t.Fatalf("second scrape: status %d, body:\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "network_ups_tools_battery_charge") {
		t.Fatalf("second scrape misses battery.charge (stale config from first request?):\n%s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "network_ups_tools_ups_load") {
		t.Fatalf("second scrape exported first request's variables:\n%s", rec.Body.String())
	}
}

func TestScrapeServerportValidation(t *testing.T) {
	// Base config points at a server that hangs after the handshake, so a
	// scrape that ignores the serverport query cannot look successful.
	hanging := startFakeNUT(t, true)
	reachable := startFakeNUT(t, false)
	handler := newMetricsHandler(baseOpts(hanging.port()), 10*time.Second)

	rec := scrape(t, handler, "?server=127.0.0.1&serverport="+itoa(reachable.port()))
	if rec.Code != http.StatusOK {
		t.Fatalf("valid serverport rejected or ignored: status %d, body:\n%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "network_ups_tools_ups_load") {
		t.Fatalf("valid serverport scrape missed metrics:\n%s", rec.Body.String())
	}

	for _, bad := range []string{"abc", "0", "99999", "-1"} {
		rec := scrape(t, handler, "?server=127.0.0.1&serverport="+bad)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("serverport=%q: want 400, got %d, body:\n%s", bad, rec.Code, rec.Body.String())
		}
	}
}

func TestClientCancellationClosesConnection(t *testing.T) {
	fake := startFakeNUT(t, true)
	handler := newMetricsHandler(baseOpts(fake.port()), 30*time.Second)
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/ups_metrics", nil)
	if err != nil {
		t.Fatal(err)
	}
	errc := make(chan error, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		errc <- err
	}()

	// Let the scrape connect and hang on LIST VAR, then walk away.
	time.Sleep(250 * time.Millisecond)
	cancel()

	select {
	case <-fake.closed:
	case <-time.After(1 * time.Second):
		t.Fatal("cancelled request did not close the NUT connection within 1s")
	}
	<-errc
}

func itoa(i int) string {
	return strconv.Itoa(i)
}
