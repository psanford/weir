// Package wiretest provides a fake Wayland compositor for testing clients
// of the wire package and generated protocol bindings without a real
// compositor. The fake server speaks the raw wire format over one end of a
// socketpair.
package wiretest

import (
	"encoding/binary"
	"net"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/psanford/weir/wire"
)

const headerSize = 8

var order = binary.NativeEndian

// Server is the compositor end of a socketpair connected to a wire.Conn.
type Server struct {
	t    *testing.T
	Sock *net.UnixConn
}

// Pair returns a connected client and fake server. Both ends are closed
// automatically when the test finishes.
func Pair(t *testing.T) (*wire.Conn, *Server) {
	t.Helper()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	toConn := func(fd int, name string) *net.UnixConn {
		f := os.NewFile(uintptr(fd), name)
		defer f.Close()
		c, err := net.FileConn(f)
		if err != nil {
			t.Fatalf("FileConn: %v", err)
		}
		return c.(*net.UnixConn)
	}
	client := wire.NewConn(toConn(fds[0], "client"))
	server := &Server{t: t, Sock: toConn(fds[1], "server")}
	t.Cleanup(func() {
		client.Close()
		server.Sock.Close()
	})
	return client, server
}

// Msg is a decoded message received from the client.
type Msg struct {
	Object uint32
	Opcode uint16
	Body   []byte
}

// Decoder returns a decoder positioned at the start of the message body.
// File descriptor arguments cannot be decoded this way; use RecvFds.
func (m Msg) Decoder() *wire.Decoder { return wire.NewDecoder(m.Body) }

// Send writes a raw event to the client, optionally passing file
// descriptors as ancillary data.
func (s *Server) Send(objID uint32, opcode uint16, e *wire.Encoder, fds ...int) {
	s.t.Helper()
	body := e.Bytes()
	size := headerSize + len(body)
	msg := make([]byte, 0, size)
	msg = order.AppendUint32(msg, objID)
	msg = order.AppendUint32(msg, uint32(size)<<16|uint32(opcode))
	msg = append(msg, body...)
	var oob []byte
	if len(fds) > 0 {
		oob = syscall.UnixRights(fds...)
	}
	if _, _, err := s.Sock.WriteMsgUnix(msg, oob, nil); err != nil {
		s.t.Fatalf("wiretest: send: %v", err)
	}
}

// Recv reads one message from the client. It fails the test after a 5
// second timeout.
func (s *Server) Recv() Msg {
	s.t.Helper()
	s.Sock.SetReadDeadline(time.Now().Add(5 * time.Second))
	hdr := make([]byte, headerSize)
	if _, err := s.readFull(hdr); err != nil {
		s.t.Fatalf("wiretest: read header: %v", err)
	}
	objID := order.Uint32(hdr)
	sizeOp := order.Uint32(hdr[4:])
	size := int(sizeOp >> 16)
	body := make([]byte, size-headerSize)
	if _, err := s.readFull(body); err != nil {
		s.t.Fatalf("wiretest: read body: %v", err)
	}
	return Msg{Object: objID, Opcode: uint16(sizeOp & 0xffff), Body: body}
}

// RecvWithFds reads one message and any file descriptors sent with it.
func (s *Server) RecvWithFds(bufSize int) (Msg, []int) {
	s.t.Helper()
	s.Sock.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, bufSize)
	oob := make([]byte, 256)
	n, oobn, _, _, err := s.Sock.ReadMsgUnix(buf, oob)
	if err != nil {
		s.t.Fatalf("wiretest: recvmsg: %v", err)
	}
	var fds []int
	if oobn > 0 {
		cmsgs, err := syscall.ParseSocketControlMessage(oob[:oobn])
		if err != nil {
			s.t.Fatalf("wiretest: parse cmsg: %v", err)
		}
		for _, cmsg := range cmsgs {
			got, err := syscall.ParseUnixRights(&cmsg)
			if err == nil {
				fds = append(fds, got...)
			}
		}
	}
	if n < headerSize {
		s.t.Fatalf("wiretest: short read: %d bytes", n)
	}
	objID := order.Uint32(buf)
	sizeOp := order.Uint32(buf[4:])
	return Msg{Object: objID, Opcode: uint16(sizeOp & 0xffff), Body: buf[headerSize:n]}, fds
}

// HasData reports whether the server side of the socket has unread data
// available right now, without consuming it.
func (s *Server) HasData() bool {
	raw, err := s.Sock.SyscallConn()
	if err != nil {
		return false
	}
	var n int
	raw.Control(func(fd uintptr) {
		buf := make([]byte, 1)
		n, _, _ = syscall.Recvfrom(int(fd), buf, syscall.MSG_PEEK|syscall.MSG_DONTWAIT)
	})
	return n > 0
}

func (s *Server) readFull(b []byte) (int, error) {
	n := 0
	for n < len(b) {
		m, err := s.Sock.Read(b[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}
