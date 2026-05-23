// Command weirctl controls a running weir window manager over its unix
// control socket.
//
// Usage:
//
//	weirctl <command> [args...]   run a weir command
//	weirctl get state             print the full state as JSON
//	weirctl subscribe             stream state-change events as JSON lines
//	weirctl help                  list available commands
//
// The socket is found via $WEIRSOCK or derived from $WAYLAND_DISPLAY; pass
// -socket to override.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/psanford/weir/ipc"
)

func main() {
	socket := flag.String("socket", "", "control socket path (default: derived from the environment)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: weirctl [-socket path] <command> [args...]\n")
		fmt.Fprintf(os.Stderr, "       weirctl subscribe\n")
		fmt.Fprintf(os.Stderr, "run \"weirctl help\" for the list of commands\n")
	}
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(2)
	}

	path := *socket
	if path == "" {
		var err error
		path, err = ipc.SocketPath()
		if err != nil {
			fatal(err)
		}
	}
	conn, err := dialWithRetry(path)
	if err != nil {
		fatal(fmt.Errorf("connecting to weir at %s: %w (is weir running?)", path, err))
	}
	defer conn.Close()

	if len(args) == 1 && args[0] == "subscribe" {
		subscribe(conn)
		return
	}

	enc := json.NewEncoder(conn)
	if err := enc.Encode(ipc.Request{Command: args}); err != nil {
		fatal(err)
	}
	var resp ipc.Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		fatal(fmt.Errorf("reading response: %w", err))
	}
	if resp.Output != "" {
		fmt.Println(resp.Output)
	}
	if !resp.Success {
		fmt.Fprintln(os.Stderr, "weirctl:", resp.Error)
		os.Exit(1)
	}
}

// subscribe streams event lines to stdout until the connection closes.
func subscribe(conn net.Conn) {
	if err := json.NewEncoder(conn).Encode(ipc.Request{Subscribe: true}); err != nil {
		fatal(err)
	}
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(os.Stdout)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			w.Write(line)
			w.Flush()
		}
		if err != nil {
			if err != io.EOF {
				fatal(err)
			}
			return
		}
	}
}

// dialWithRetry connects to the control socket, retrying for a short period
// if weir has not created it yet. This lets init scripts run weirctl
// immediately after starting weir without a sleep.
func dialWithRetry(path string) (net.Conn, error) {
	deadline := time.Now().Add(3 * time.Second)
	for {
		conn, err := net.Dial("unix", path)
		if err == nil {
			return conn, nil
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "weirctl:", err)
	os.Exit(1)
}
