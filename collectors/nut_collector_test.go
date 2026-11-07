package collectors

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
)

// fakeNut answers the commands go.nut sends to list the UPSes, with a single UPS named "ups"
func fakeNut(t *testing.T) int {
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
				lines := bufio.NewScanner(conn)
				for lines.Scan() {
					cmd := lines.Text()
					switch {
					case cmd == "LIST UPS":
						fmt.Fprint(conn, "BEGIN LIST UPS\nUPS ups \"bench\"\nEND LIST UPS\n")
					case strings.HasPrefix(cmd, "LIST "):
						fmt.Fprintf(conn, "BEGIN %s\nEND %s\n", cmd, cmd)
					case cmd == "GET NUMLOGINS ups":
						fmt.Fprint(conn, "NUMLOGINS ups 0\n")
					case cmd == "GET UPSDESC ups":
						fmt.Fprint(conn, "UPSDESC ups \"bench\"\n")
					case cmd == "LOGOUT":
						fmt.Fprint(conn, "OK Goodbye\n")
						return
					default:
						fmt.Fprint(conn, "ERR UNKNOWN-COMMAND\n")
					}
				}
			}(conn)
		}
	}()

	return listener.Addr().(*net.TCPAddr).Port
}

// The UPS name is checked on the configured port, not on the default one
func TestUPSNameOnServerPort(t *testing.T) {
	port := fakeNut(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if _, err := NewNutCollector(NutCollectorOpts{Server: "127.0.0.1", ServerPort: port, Ups: "ups"}, logger); err != nil {
		t.Errorf("valid UPS refused: %v", err)
	}
	if _, err := NewNutCollector(NutCollectorOpts{Server: "127.0.0.1", ServerPort: port, Ups: "nope"}, logger); err == nil {
		t.Errorf("unknown UPS accepted: the name was not checked against the server on port %d", port)
	}
}
