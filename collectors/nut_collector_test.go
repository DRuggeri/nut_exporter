package collectors

import (
	"bufio"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// A collector used directly (no HTTP layer) must still be bounded: without an
// explicit context, DefaultScrapeTimeout caps the whole collection.
func TestCollectIsBoundedWithoutContext(t *testing.T) {
	original := DefaultScrapeTimeout
	DefaultScrapeTimeout = 300 * time.Millisecond
	t.Cleanup(func() { DefaultScrapeTimeout = original })

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			// Handshake completes; nothing else ever gets answered.
			go func(conn net.Conn) {
				defer conn.Close()
				reader := bufio.NewReader(conn)
				for i := 0; i < 2; i++ {
					if _, err := reader.ReadString('\n'); err != nil {
						return
					}
					if _, err := io.WriteString(conn, "2.8.0\n"); err != nil {
						return
					}
				}
				_, _ = io.Copy(io.Discard, reader)
			}(conn)
		}
	}()

	collector, err := NewNutCollector(NutCollectorOpts{
		Namespace:  "test",
		Server:     "127.0.0.1",
		ServerPort: listener.Addr().(*net.TCPAddr).Port,
	}, slog.New(slog.NewTextHandler(discardWriter{}, nil)))
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		collector.Collect(make(chan<- prometheus.Metric, 16))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("collection without context ran past the default budget")
	}
}

// IsValidUPSName must dial the configured ServerPort. The historical
// implementation always dialed the default port, so a reachable server on a
// custom port was reported as a connection error instead of answering the
// validity question.
func TestIsValidUPSNameUsesConfiguredPort(t *testing.T) {
	if testing.Short() {
		t.Skip("integration-style test")
	}
	port := startCollectorFakeNUT(t)

	collector, err := NewNutCollector(NutCollectorOpts{
		Namespace:  "test",
		Server:     "127.0.0.1",
		ServerPort: port,
		Ups:        "",
	}, slog.New(slog.NewTextHandler(discardWriter{}, nil)))
	if err != nil {
		t.Fatal(err)
	}

	valid, err := collector.IsValidUPSName("other")
	if err != nil {
		t.Fatalf("validity check failed to reach the configured port: %v", err)
	}
	if valid {
		t.Fatal("unknown UPS name reported as valid")
	}
}

var collectorFakeReplies = map[string]string{
	"VER":                    "2.8.0",
	"NETVER":                 "2.8.0",
	"LIST UPS":               "BEGIN LIST UPS\nUPS demo \"Demo UPS\"\nEND LIST UPS",
	"LIST CLIENT demo":       "BEGIN LIST CLIENT demo\nEND LIST CLIENT demo",
	"LIST CMD demo":          "BEGIN LIST CMD demo\nEND LIST CMD demo",
	"GET UPSDESC demo":       "UPSDESC demo \"Demo UPS\"",
	"GET NUMLOGINS demo":     "NUMLOGINS demo 1",
	"LIST VAR demo":          "BEGIN LIST VAR demo\nVAR demo ups.load \"42\"\nEND LIST VAR demo",
	"GET DESC demo ups.load": "DESC demo ups.load \"Output load\"",
	"GET TYPE demo ups.load": "TYPE demo ups.load INTEGER",
	"LOGOUT":                 "OK Goodbye",
}

func startCollectorFakeNUT(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				reader := bufio.NewReader(conn)
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					reply, ok := collectorFakeReplies[strings.TrimSuffix(line, "\n")]
					if !ok {
						reply = "ERR UNKNOWN-COMMAND"
					}
					if _, err := io.WriteString(conn, reply+"\n"); err != nil {
						return
					}
				}
			}(conn)
		}
	}()
	return listener.Addr().(*net.TCPAddr).Port
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
