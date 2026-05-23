package bridge

import (
	"fmt"
	"testing"

	"github.com/psanford/weir/core"
	"github.com/psanford/weir/protocols/river"
	"github.com/psanford/weir/wire"
	"github.com/psanford/weir/wire/wiretest"
)

// Request and event opcodes for the interfaces the fake river implements.
// Derived from declaration order in the protocol XML; the generated
// bindings' Dispatch methods are the reference.
const (
	// river_window_manager_v1
	wmReqStop            = 0
	wmReqDestroy         = 1
	wmReqManageFinish    = 2
	wmReqManageDirty     = 3
	wmReqRenderFinish    = 4
	wmReqGetShellSurface = 5
	wmReqExitSession     = 6

	wmEvUnavailable     = 0
	wmEvFinished        = 1
	wmEvManageStart     = 2
	wmEvRenderStart     = 3
	wmEvSessionLocked   = 4
	wmEvSessionUnlocked = 5
	wmEvWindow          = 6
	wmEvOutput          = 7
	wmEvSeat            = 8

	// river_window_v1
	winReqDestroy             = 0
	winReqClose               = 1
	winReqGetNode             = 2
	winReqProposeDimensions   = 3
	winReqHide                = 4
	winReqShow                = 5
	winReqUseCsd              = 6
	winReqUseSsd              = 7
	winReqSetBorders          = 8
	winReqSetTiled            = 9
	winReqGetDecorationAbove  = 10
	winReqGetDecorationBelow  = 11
	winReqInformResizeStart   = 12
	winReqInformResizeEnd     = 13
	winReqSetCapabilities     = 14
	winReqInformMaximized     = 15
	winReqInformUnmaximized   = 16
	winReqInformFullscreen    = 17
	winReqInformNotFullscreen = 18
	winReqFullscreen          = 19
	winReqExitFullscreen      = 20
	winReqSetClipBox          = 21
	winReqSetContentClipBox   = 22
	winReqSetDimensionBounds  = 23

	winEvClosed         = 0
	winEvDimensionsHint = 1
	winEvDimensions     = 2
	winEvAppID          = 3
	winEvTitle          = 4
	winEvParent         = 5

	// river_node_v1
	nodeReqDestroy     = 0
	nodeReqSetPosition = 1
	nodeReqPlaceTop    = 2
	nodeReqPlaceBottom = 3
	nodeReqPlaceAbove  = 4
	nodeReqPlaceBelow  = 5

	// river_output_v1
	outReqDestroy = 0

	outEvRemoved    = 0
	outEvWlOutput   = 1
	outEvPosition   = 2
	outEvDimensions = 3

	// river_seat_v1
	seatReqDestroy           = 0
	seatReqFocusWindow       = 1
	seatReqFocusShellSurface = 2
	seatReqClearFocus        = 3

	seatEvRemoved           = 0
	seatEvWlSeat            = 1
	seatEvPointerEnter      = 2
	seatEvPointerLeave      = 3
	seatEvWindowInteraction = 4
)

// reqKind classifies a request for sequencing validation.
type reqKind int

const (
	kindNeutral reqKind = iota // destroy, get_node, etc: legal anywhere
	kindManage                 // window management state: only inside a manage sequence
	kindRender                 // rendering state: only inside a manage or render sequence
)

// request is a request received from the bridge, tagged with the interface
// of the object it was sent to.
type request struct {
	iface  string
	object uint32
	opcode uint16
	body   []byte
}

func (r request) String() string {
	return fmt.Sprintf("%s@%d.%d", r.iface, r.object, r.opcode)
}

func (r request) decoder() *wire.Decoder { return wire.NewDecoder(r.body) }

// classify returns the sequencing class of a request, derived from the
// "This request modifies ... state" annotations in the protocol.
func classify(iface string, opcode uint16) reqKind {
	switch iface {
	case "river_window_manager_v1":
		return kindNeutral
	case "river_window_v1":
		switch opcode {
		case winReqDestroy, winReqGetNode, winReqGetDecorationAbove, winReqGetDecorationBelow:
			return kindNeutral
		case winReqHide, winReqShow, winReqSetBorders, winReqSetClipBox, winReqSetContentClipBox:
			return kindRender
		default:
			return kindManage
		}
	case "river_node_v1":
		switch opcode {
		case nodeReqDestroy:
			return kindNeutral
		default:
			return kindRender
		}
	case "river_seat_v1":
		switch opcode {
		case seatReqDestroy, seatReqGetPointerBinding:
			return kindNeutral
		default:
			return kindManage
		}
	case "river_output_v1":
		return kindNeutral
	case "river_xkb_bindings_v1":
		// Object creation only; the created bindings are enabled
		// separately.
		return kindNeutral
	case "river_xkb_binding_v1", "river_pointer_binding_v1":
		switch opcode {
		case 0: // destroy
			return kindNeutral
		default: // set_layout_override / enable / disable
			return kindManage
		}
	case "river_layer_shell_v1":
		return kindNeutral // destroy / get_output / get_seat
	case "river_layer_shell_output_v1":
		switch opcode {
		case lsOutputReqSetDefault:
			return kindManage
		default: // destroy
			return kindNeutral
		}
	case "river_layer_shell_seat_v1":
		return kindNeutral
	case "river_input_manager_v1", "river_input_device_v1", "river_xkb_config_v1",
		"river_xkb_keymap_v1", "river_xkb_keyboard_v1":
		// Input configuration is not window management state.
		return kindNeutral
	}
	return kindNeutral
}

// seqState is the fake river's view of where the client is in the
// manage/render cycle.
type seqState int

const (
	seqIdle seqState = iota
	seqManage
	seqRender
)

// fakeRiver is a minimal river compositor for bridge tests. It tracks
// object IDs the client allocates, validates request sequencing, and lets
// tests script compositor behavior.
type fakeRiver struct {
	t      *testing.T
	conn   *wire.Conn
	server *wiretest.Server
	bridge *Bridge

	// Object IDs. Server-allocated IDs start at 0xff000000.
	nextServerID   uint32
	registryID     uint32
	wmID           uint32
	xkbID          uint32
	layerShellID   uint32
	inputManagerID uint32
	xkbConfigID    uint32
	// seatID is the most recently added seat's object ID.
	seatID uint32
	// layerShellOutputs maps river_output_v1 IDs to the
	// river_layer_shell_output_v1 the client created for them.
	layerShellOutputs map[uint32]uint32
	// layerShellSeatID is the river_layer_shell_seat_v1 the client created
	// for the seat, or 0.
	layerShellSeatID uint32
	// ifaces maps every known object ID to its interface name so requests
	// can be classified. Populated from bind/get_node/new_id requests and
	// from the server's own object announcements.
	ifaces map[uint32]string

	seq seqState

	// received accumulates every request seen, in order.
	received []request
}

func newFakeRiver(t *testing.T) (*fakeRiver, *Bridge) {
	conn, server := wiretest.Pair(t)
	model := core.NewModel()
	b := New(conn, model, nil)
	f := &fakeRiver{
		t:                 t,
		conn:              conn,
		server:            server,
		bridge:            b,
		nextServerID:      0xff000000,
		ifaces:            map[uint32]string{1: "wl_display"},
		layerShellOutputs: make(map[uint32]uint32),
	}
	f.bootstrap()
	return f, b
}

// bootstrap runs the registry handshake concurrently with the bridge's
// Bootstrap call, which performs blocking round trips.
func (f *fakeRiver) bootstrap() {
	done := make(chan error, 1)
	go func() { done <- f.bridge.Bootstrap() }()

	// get_registry
	m := f.server.Recv()
	if m.Object != 1 || m.Opcode != 1 {
		f.t.Errorf("expected get_registry, got %d.%d", m.Object, m.Opcode)
	}
	d := m.Decoder()
	f.registryID, _ = d.Uint()
	f.ifaces[f.registryID] = "wl_registry"
	// Announce the window manager, xkb bindings, and layer shell globals.
	for i, iface := range []struct {
		name    string
		version uint32
	}{
		{river.WindowManagerV1Name, river.WindowManagerV1Version},
		{river.XkbBindingsV1Name, river.XkbBindingsV1Version},
		{river.LayerShellV1Name, river.LayerShellV1Version},
		{river.InputManagerV1Name, river.InputManagerV1Version},
		{river.XkbConfigV1Name, river.XkbConfigV1Version},
	} {
		e := &wire.Encoder{}
		e.PutUint(uint32(7 + i))
		e.PutString(iface.name)
		e.PutUint(iface.version)
		f.server.Send(f.registryID, 0, e)
	}
	// sync #1
	f.respondSync()
	// bind x5
	for i := 0; i < 5; i++ {
		m = f.server.Recv()
		if m.Object != f.registryID || m.Opcode != 0 {
			f.t.Errorf("expected registry.bind, got %d.%d", m.Object, m.Opcode)
		}
		d = m.Decoder()
		d.Uint() // global name
		bindIface, _, _ := d.String()
		d.Uint() // version
		id, _ := d.Uint()
		f.ifaces[id] = bindIface
		switch bindIface {
		case river.WindowManagerV1Name:
			f.wmID = id
		case river.XkbBindingsV1Name:
			f.xkbID = id
		case river.LayerShellV1Name:
			f.layerShellID = id
		case river.InputManagerV1Name:
			f.inputManagerID = id
		case river.XkbConfigV1Name:
			f.xkbConfigID = id
		}
	}
	if f.wmID == 0 || f.xkbID == 0 || f.layerShellID == 0 || f.inputManagerID == 0 || f.xkbConfigID == 0 {
		f.t.Errorf("client did not bind all globals (wm=%d xkb=%d ls=%d im=%d xc=%d)",
			f.wmID, f.xkbID, f.layerShellID, f.inputManagerID, f.xkbConfigID)
	}
	// sync #2
	f.respondSync()

	if err := <-done; err != nil {
		f.t.Fatalf("Bootstrap: %v", err)
	}
}

// respondSync reads a wl_display.sync request and answers it.
func (f *fakeRiver) respondSync() {
	m := f.server.Recv()
	if m.Object != 1 || m.Opcode != 0 {
		f.t.Fatalf("expected wl_display.sync, got %d.%d", m.Object, m.Opcode)
	}
	d := m.Decoder()
	cbID, _ := d.Uint()
	e := &wire.Encoder{}
	e.PutUint(0)
	f.server.Send(cbID, 0, e) // done
	e = &wire.Encoder{}
	e.PutUint(cbID)
	f.server.Send(1, 1, e) // delete_id
}

// allocServerID returns a fresh server-allocated object ID registered under
// the given interface name.
func (f *fakeRiver) allocServerID(iface string) uint32 {
	f.nextServerID++
	f.ifaces[f.nextServerID] = iface
	return f.nextServerID
}

// serverHasData reports whether the server side of the socketpair has
// unread data.
func (f *fakeRiver) serverHasData() bool {
	return f.server.HasData()
}

// handleRequest validates and records one request from the bridge.
func (f *fakeRiver) handleRequest(m wiretest.Msg) {
	f.t.Helper()
	iface := f.ifaces[m.Object]
	if iface == "" {
		f.t.Fatalf("request to unknown object %d opcode %d", m.Object, m.Opcode)
	}
	req := request{iface: iface, object: m.Object, opcode: m.Opcode, body: m.Body}
	f.received = append(f.received, req)

	// Sequencing validation.
	switch classify(iface, m.Opcode) {
	case kindManage:
		if f.seq != seqManage {
			f.t.Errorf("window-management request %v sent outside a manage sequence (state %d)", req, f.seq)
		}
	case kindRender:
		if f.seq != seqManage && f.seq != seqRender {
			f.t.Errorf("rendering request %v sent outside a manage or render sequence (state %d)", req, f.seq)
		}
	}

	// Track state transitions and new object registrations.
	switch {
	case iface == "river_window_manager_v1" && m.Opcode == wmReqManageFinish:
		if f.seq != seqManage {
			f.t.Errorf("manage_finish outside a manage sequence")
		}
		f.seq = seqIdle
	case iface == "river_window_manager_v1" && m.Opcode == wmReqRenderFinish:
		if f.seq != seqRender && f.seq != seqManage {
			f.t.Errorf("render_finish outside a render sequence")
		}
		f.seq = seqIdle
	case iface == "river_window_v1" && m.Opcode == winReqGetNode:
		d := req.decoder()
		id, _ := d.Uint()
		f.ifaces[id] = "river_node_v1"
	case iface == "river_xkb_bindings_v1" && m.Opcode == xkbBindingsReqGetXkbBinding:
		d := req.decoder()
		d.Object() // seat
		id, _ := d.Uint()
		f.ifaces[id] = "river_xkb_binding_v1"
	case iface == "river_seat_v1" && m.Opcode == seatReqGetPointerBinding:
		d := req.decoder()
		id, _ := d.Uint()
		f.ifaces[id] = "river_pointer_binding_v1"
	case iface == "river_layer_shell_v1" && m.Opcode == lsReqGetOutput:
		d := req.decoder()
		id, _ := d.Uint()
		outID, _ := d.Object()
		f.ifaces[id] = "river_layer_shell_output_v1"
		f.layerShellOutputs[outID] = id
	case iface == "river_layer_shell_v1" && m.Opcode == lsReqGetSeat:
		d := req.decoder()
		id, _ := d.Uint()
		f.ifaces[id] = "river_layer_shell_seat_v1"
		f.layerShellSeatID = id
	case iface == "river_xkb_config_v1" && m.Opcode == 2: // create_keymap
		d := req.decoder()
		id, _ := d.Uint()
		f.ifaces[id] = "river_xkb_keymap_v1"
	case iface == "wl_registry" && m.Opcode == 0:
		// bind: record the new object's interface.
		d := req.decoder()
		d.Uint()
		bindIface, _, _ := d.String()
		d.Uint()
		id, _ := d.Uint()
		f.ifaces[id] = bindIface
	}
}

// --- compositor actions -----------------------------------------------------

// addOutput announces a new output with the given geometry and returns its
// server-side object ID.
func (f *fakeRiver) addOutput(x, y, w, h int32) uint32 {
	id := f.allocServerID("river_output_v1")
	e := &wire.Encoder{}
	e.PutUint(id)
	f.server.Send(f.wmID, wmEvOutput, e)
	e = &wire.Encoder{}
	e.PutInt(x)
	e.PutInt(y)
	f.server.Send(id, outEvPosition, e)
	e = &wire.Encoder{}
	e.PutInt(w)
	e.PutInt(h)
	f.server.Send(id, outEvDimensions, e)
	return id
}

// removeOutput sends the removed event for an output.
func (f *fakeRiver) removeOutput(id uint32) {
	f.server.Send(id, outEvRemoved, &wire.Encoder{})
}

// addWindow announces a new window and returns its server-side object ID.
func (f *fakeRiver) addWindow() uint32 {
	id := f.allocServerID("river_window_v1")
	e := &wire.Encoder{}
	e.PutUint(id)
	f.server.Send(f.wmID, wmEvWindow, e)
	return id
}

// closeWindow sends the closed event for a window.
func (f *fakeRiver) closeWindow(id uint32) {
	f.server.Send(id, winEvClosed, &wire.Encoder{})
}

// addSeat announces a new seat and returns its server-side object ID.
func (f *fakeRiver) addSeat() uint32 {
	id := f.allocServerID("river_seat_v1")
	e := &wire.Encoder{}
	e.PutUint(id)
	f.server.Send(f.wmID, wmEvSeat, e)
	f.seatID = id
	return id
}

// windowDimensions reports a window's actual dimensions (render state).
func (f *fakeRiver) windowDimensions(id uint32, w, h int32) {
	e := &wire.Encoder{}
	e.PutInt(w)
	e.PutInt(h)
	f.server.Send(id, winEvDimensions, e)
}

// manageCycle sends manage_start, delivers all pending events to the
// bridge, and collects the bridge's requests up to and including
// manage_finish. It returns the requests sent during the manage sequence.
func (f *fakeRiver) manageCycle() []request {
	f.t.Helper()
	f.server.Send(f.wmID, wmEvManageStart, &wire.Encoder{})
	f.seq = seqManage
	start := len(f.received)
	f.deliverAndCollect(func() bool { return f.seq == seqIdle })
	return f.received[start:]
}

// renderCycle sends render_start and collects requests up to and including
// render_finish.
func (f *fakeRiver) renderCycle() []request {
	f.t.Helper()
	f.server.Send(f.wmID, wmEvRenderStart, &wire.Encoder{})
	f.seq = seqRender
	start := len(f.received)
	f.deliverAndCollect(func() bool { return f.seq == seqIdle })
	return f.received[start:]
}

// deliverAndCollect lets the client process everything the server has sent,
// then reads the client's requests until done() reports completion.
//
// The exchange is fully synchronous: the server's events are already in the
// socket, one Dispatch reads and handles them all (firing the bridge's
// manage/render logic), and the bridge's buffered requests are flushed and
// read back. If done() is not satisfied after that, the bridge failed to
// finish the sequence and the test fails rather than deadlocking.
func (f *fakeRiver) deliverAndCollect(done func() bool) {
	f.t.Helper()
	if _, err := f.conn.Dispatch(); err != nil {
		f.t.Fatalf("dispatch: %v", err)
	}
	for {
		n, err := f.conn.DispatchPending()
		if err != nil {
			f.t.Fatalf("dispatch pending: %v", err)
		}
		if n == 0 {
			break
		}
	}
	if err := f.conn.Flush(); err != nil {
		f.t.Fatalf("flush: %v", err)
	}
	for f.serverHasData() {
		f.handleRequest(f.server.Recv())
	}
	if !done() {
		f.t.Fatalf("sequence did not complete; requests so far: %v", f.received)
	}
}

// collect flushes the client's buffered requests and reads everything the
// server can see, without delivering any new events to the client. Use this
// when the client has produced requests outside a manage/render cycle (e.g.
// manage_dirty after an IPC command).
func (f *fakeRiver) collect() []request {
	f.t.Helper()
	if err := f.conn.Flush(); err != nil {
		f.t.Fatalf("flush: %v", err)
	}
	start := len(f.received)
	for f.serverHasData() {
		f.handleRequest(f.server.Recv())
	}
	return f.received[start:]
}

// find returns the requests matching the given interface and opcode.
func find(reqs []request, iface string, opcode uint16) []request {
	var out []request
	for _, r := range reqs {
		if r.iface == iface && r.opcode == opcode {
			out = append(out, r)
		}
	}
	return out
}
