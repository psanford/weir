// Command weirctl controls a running weir window manager over its unix
// control socket.
//
// Usage:
//
//	weirctl <command> [args...]        run a weir command
//	weirctl get state                  print the full state as JSON
//	weirctl subscribe                  stream state-change events as JSON lines
//	weirctl wait-for-socket [seconds]  block until weir's socket is up
//	weirctl help                       list available commands
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
	"strconv"
	"time"

	"github.com/psanford/weir/ipc"
)

func main() {
	socket := flag.String("socket", "", "control socket path (default: derived from the environment)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: weirctl [-socket path] <command> [args...]\n\n")
		fmt.Fprintf(os.Stderr, "examples:\n")
		fmt.Fprintf(os.Stderr, "  weirctl help                  every command, grouped by topic\n")
		fmt.Fprintf(os.Stderr, "  weirctl get state | jq .      everything weir knows, as JSON\n")
		fmt.Fprintf(os.Stderr, "  weirctl set                   every option for \"set\"\n")
		fmt.Fprintf(os.Stderr, "  weirctl bind Super+j focus next\n")
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
	if len(args) >= 1 && args[0] == "wait-for-socket" {
		waitForSocket(path, args[1:])
		return
	}

	conn, err := net.Dial("unix", path)
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
	if len(args) == 1 && args[0] == "help" {
		fmt.Print(localHelp)
	}
}

// localHelp documents the commands handled by weirctl itself rather than
// sent to weir. Appended to the output of "weirctl help".
const localHelp = `
weirctl:
  subscribe                                    stream a state snapshot on every change
  wait-for-socket [timeout-seconds]            block until weir's control socket is up
  -socket <path>                               use a specific control socket
`

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

// waitForSocket blocks until weir's control socket accepts a connection or
// the timeout expires. Use it in init scripts between starting weir and the
// first configuration command. Every other weirctl invocation fails
// immediately if weir is not running.
func waitForSocket(path string, args []string) {
	timeout := 5 * time.Second
	if len(args) == 1 {
		secs, err := strconv.ParseFloat(args[0], 64)
		if err != nil || secs <= 0 {
			fatal(fmt.Errorf("invalid timeout %q (want seconds)", args[0]))
		}
		timeout = time.Duration(secs * float64(time.Second))
	} else if len(args) > 1 {
		fatal(fmt.Errorf("usage: weirctl wait-for-socket [timeout-seconds]"))
	}
	deadline := time.Now().Add(timeout)
	for {
		conn, err := net.Dial("unix", path)
		if err == nil {
			conn.Close()
			return
		}
		if time.Now().After(deadline) {
			fatal(fmt.Errorf("timed out after %v waiting for weir at %s: %w", timeout, path, err))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "weirctl:", err)
	os.Exit(1)
}
