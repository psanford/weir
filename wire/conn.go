package wire

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// Conn is a client connection to a Wayland compositor.
//
// Conn is not safe for concurrent use: all requests, dispatching, and
// flushing must happen from a single goroutine. This matches the
// single-threaded transaction model of the river window management protocol.
type Conn struct {
	sock *net.UnixConn

	// Display is object 1, always present.
	Display *Display

	objects map[uint32]Object
	nextID  uint32

	// Outgoing request buffer, flushed explicitly.
	out    []byte
	outFds []int

	// Incoming byte buffer. in[inStart:inEnd] is unprocessed data.
	in      []byte
	inStart int
	inEnd   int

	// recvFds is the queue of file descriptors received as ancillary data
	// and not yet consumed by an fd argument.
	recvFds []int

	// err is the first fatal error encountered; once set the connection is
	// unusable.
	err error
}

// Connect connects to the Wayland display named by the environment:
// $WAYLAND_SOCKET (a pre-connected fd passed by a parent compositor) is
// preferred, then $WAYLAND_DISPLAY resolved against $XDG_RUNTIME_DIR.
func Connect() (*Conn, error) {
	if v := os.Getenv("WAYLAND_SOCKET"); v != "" {
		fd, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("wire: invalid WAYLAND_SOCKET %q", v)
		}
		// The fd is intended for us alone; clear the variable so child
		// processes don't try to use it.
		os.Unsetenv("WAYLAND_SOCKET")
		syscall.CloseOnExec(fd)
		f := os.NewFile(uintptr(fd), "wayland")
		fc, err := net.FileConn(f)
		f.Close() // FileConn dups the fd
		if err != nil {
			return nil, fmt.Errorf("wire: WAYLAND_SOCKET: %w", err)
		}
		uc, ok := fc.(*net.UnixConn)
		if !ok {
			fc.Close()
			return nil, fmt.Errorf("wire: WAYLAND_SOCKET is not a unix socket")
		}
		return NewConn(uc), nil
	}

	display := os.Getenv("WAYLAND_DISPLAY")
	if display == "" {
		display = "wayland-0"
	}
	path := display
	if !filepath.IsAbs(path) {
		dir := os.Getenv("XDG_RUNTIME_DIR")
		if dir == "" {
			return nil, errors.New("wire: XDG_RUNTIME_DIR is not set")
		}
		path = filepath.Join(dir, display)
	}
	return ConnectPath(path)
}

// ConnectPath connects to the Wayland socket at the given path.
func ConnectPath(path string) (*Conn, error) {
	uc, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("wire: connect %s: %w", path, err)
	}
	return NewConn(uc), nil
}

// NewConn wraps an existing unix socket connection. Used by Connect and by
// tests that drive the client over a socketpair.
func NewConn(sock *net.UnixConn) *Conn {
	c := &Conn{
		sock:    sock,
		objects: make(map[uint32]Object),
		nextID:  2, // 1 is wl_display
		in:      make([]byte, 0, 4096),
	}
	c.Display = &Display{}
	c.Display.Proxy.init(c, 1)
	c.objects[1] = c.Display
	return c
}

// Close closes the connection and any queued received file descriptors.
func (c *Conn) Close() error {
	for _, fd := range c.recvFds {
		syscall.Close(fd)
	}
	c.recvFds = nil
	return c.sock.Close()
}

// Err returns the first fatal error encountered on the connection, or nil.
func (c *Conn) Err() error { return c.err }

func (c *Conn) fatal(err error) error {
	if c.err == nil {
		c.err = err
	}
	return c.err
}

// SendRequest marshals and buffers a request. The message is not written to
// the socket until Flush is called. Generated request methods are the only
// intended callers.
func (c *Conn) SendRequest(sender Object, opcode uint16, e *Encoder) error {
	if c.err != nil {
		return c.err
	}
	id := sender.ID()
	if id == 0 {
		return fmt.Errorf("wire: request %d on destroyed %s object", opcode, sender.Interface())
	}
	size := headerSize + len(e.buf)
	if size > maxMessageSize {
		return c.fatal(fmt.Errorf("wire: request %s.%d too large (%d bytes)", sender.Interface(), opcode, size))
	}
	c.out = order.AppendUint32(c.out, id)
	c.out = order.AppendUint32(c.out, uint32(size)<<16|uint32(opcode))
	c.out = append(c.out, e.buf...)
	c.outFds = append(c.outFds, e.fds...)
	return nil
}

// Flush writes all buffered requests (and their file descriptors) to the
// socket.
func (c *Conn) Flush() error {
	if c.err != nil {
		return c.err
	}
	if len(c.out) == 0 && len(c.outFds) == 0 {
		return nil
	}
	var oob []byte
	if len(c.outFds) > 0 {
		oob = syscall.UnixRights(c.outFds...)
	}
	n, _, err := c.sock.WriteMsgUnix(c.out, oob, nil)
	if err != nil {
		return c.fatal(fmt.Errorf("wire: write: %w", err))
	}
	if n != len(c.out) {
		// WriteMsgUnix on a SOCK_STREAM socket writes the whole buffer or
		// fails; a short write here means something is deeply wrong.
		return c.fatal(fmt.Errorf("wire: short write: %d of %d bytes", n, len(c.out)))
	}
	c.out = c.out[:0]
	c.outFds = c.outFds[:0]
	return nil
}

// Dispatch blocks until at least one event has been read and dispatched,
// then continues dispatching any further events already buffered. Returns
// the number of events dispatched.
func (c *Conn) Dispatch() (int, error) {
	if c.err != nil {
		return 0, c.err
	}
	n := 0
	for {
		dispatched, err := c.dispatchPending()
		n += dispatched
		if err != nil {
			return n, err
		}
		if n > 0 {
			return n, nil
		}
		if err := c.read(); err != nil {
			return n, err
		}
	}
}

// DispatchPending dispatches any events already buffered without reading
// from the socket.
func (c *Conn) DispatchPending() (int, error) {
	if c.err != nil {
		return 0, c.err
	}
	return c.dispatchPending()
}

func (c *Conn) dispatchPending() (int, error) {
	n := 0
	for {
		buffered := c.inEnd - c.inStart
		if buffered < headerSize {
			break
		}
		hdr := c.in[c.inStart:]
		objID := order.Uint32(hdr)
		sizeOp := order.Uint32(hdr[4:])
		size := int(sizeOp >> 16)
		opcode := uint16(sizeOp & 0xffff)
		if size < headerSize {
			return n, c.fatal(fmt.Errorf("wire: invalid message size %d", size))
		}
		if buffered < size {
			break
		}
		body := c.in[c.inStart+headerSize : c.inStart+size]
		c.inStart += size

		obj := c.objects[objID]
		if obj == nil {
			// Events may legitimately arrive for objects the client has
			// already destroyed (the server hadn't seen the destructor
			// yet). Ignore them.
			n++
			continue
		}
		d := &Decoder{buf: body, conn: c}
		if err := obj.Dispatch(opcode, d); err != nil {
			return n, c.fatal(fmt.Errorf("wire: dispatch %s@%d opcode %d: %w", obj.Interface(), objID, opcode, err))
		}
		n++
		if c.err != nil {
			// A dispatched event (e.g. wl_display.error) marked the
			// connection as failed.
			return n, c.err
		}
	}
	return n, nil
}

// read performs one blocking read from the socket, appending data to the
// input buffer and any received file descriptors to the fd queue.
func (c *Conn) read() error {
	// Compact or grow the buffer so there is space to read into.
	if c.inStart > 0 {
		copy(c.in[:cap(c.in)], c.in[c.inStart:c.inEnd])
		c.inEnd -= c.inStart
		c.inStart = 0
	}
	if cap(c.in)-c.inEnd < 4096 {
		grown := make([]byte, c.inEnd, cap(c.in)*2+4096)
		copy(grown, c.in[:c.inEnd])
		c.in = grown
	}
	c.in = c.in[:cap(c.in)]

	// Enough ancillary space for 28 fds, matching libwayland's limit.
	oob := make([]byte, 4*28+24)
	n, oobn, _, _, err := c.sock.ReadMsgUnix(c.in[c.inEnd:], oob)
	if err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
			return c.fatal(fmt.Errorf("wire: connection closed by compositor: %w", err))
		}
		return c.fatal(fmt.Errorf("wire: read: %w", err))
	}
	if n == 0 && oobn == 0 {
		return c.fatal(errors.New("wire: connection closed by compositor"))
	}
	c.inEnd += n
	if oobn > 0 {
		if err := c.parseFds(oob[:oobn]); err != nil {
			return c.fatal(err)
		}
	}
	return nil
}

func (c *Conn) parseFds(oob []byte) error {
	cmsgs, err := syscall.ParseSocketControlMessage(oob)
	if err != nil {
		return fmt.Errorf("wire: parse control message: %w", err)
	}
	for _, cmsg := range cmsgs {
		fds, err := syscall.ParseUnixRights(&cmsg)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			syscall.CloseOnExec(fd)
		}
		c.recvFds = append(c.recvFds, fds...)
	}
	return nil
}

// Packet is a chunk of raw data read from the socket by ReadLoop, to be
// passed to Feed on the goroutine that owns the connection.
type Packet struct {
	Data []byte
	Fds  []int
	Err  error
}

// ReadLoop reads from the socket until an error occurs, sending each chunk
// of received data over ch. It is intended to run in its own goroutine: it
// touches only the socket, never the connection's buffers or object map, so
// the owning goroutine can keep using the connection concurrently. The final
// packet sent before returning carries the read error.
//
// While ReadLoop is running the owning goroutine must not call Dispatch or
// RoundTrip (which read from the socket directly); use Feed and
// DispatchPending instead.
func (c *Conn) ReadLoop(ch chan<- Packet) {
	for {
		buf := make([]byte, 4096)
		oob := make([]byte, 4*28+24)
		n, oobn, _, _, err := c.sock.ReadMsgUnix(buf, oob)
		var fds []int
		if oobn > 0 {
			if cmsgs, perr := syscall.ParseSocketControlMessage(oob[:oobn]); perr == nil {
				for _, cmsg := range cmsgs {
					if got, ferr := syscall.ParseUnixRights(&cmsg); ferr == nil {
						fds = append(fds, got...)
					}
				}
			}
		}
		if err == nil && n == 0 && oobn == 0 {
			err = errors.New("connection closed by compositor")
		}
		ch <- Packet{Data: buf[:n], Fds: fds, Err: err}
		if err != nil {
			return
		}
	}
}

// Feed appends data read by ReadLoop to the connection's input buffer and fd
// queue. Call DispatchPending afterwards to process any complete messages.
func (c *Conn) Feed(p Packet) error {
	if p.Err != nil {
		return c.fatal(fmt.Errorf("wire: read: %w", p.Err))
	}
	for _, fd := range p.Fds {
		syscall.CloseOnExec(fd)
	}
	c.recvFds = append(c.recvFds, p.Fds...)
	// Compact before appending so the buffer doesn't grow without bound.
	if c.inStart > 0 {
		copy(c.in[:cap(c.in)], c.in[c.inStart:c.inEnd])
		c.inEnd -= c.inStart
		c.inStart = 0
	}
	c.in = append(c.in[:c.inEnd], p.Data...)
	c.inEnd += len(p.Data)
	return nil
}

// RoundTrip flushes all buffered requests and blocks until the compositor
// has processed them, dispatching any events that arrive in the meantime.
func (c *Conn) RoundTrip() error {
	done := false
	cb := c.Display.Sync()
	cb.OnDone = func(uint32) { done = true }
	if err := c.Flush(); err != nil {
		return err
	}
	for !done {
		if _, err := c.Dispatch(); err != nil {
			return err
		}
	}
	return nil
}
