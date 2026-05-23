// Package bridge connects the pure window-management core to a river
// compositor over the river-window-management-v1 protocol.
//
// The bridge owns the protocol state machine (the manage/render sequence
// loop) and the mapping between protocol objects and core IDs. It makes no
// policy decisions: it translates protocol events into core model events and
// the core's computed Arrangement into protocol requests.
package bridge

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/psanford/weir/core"
	"github.com/psanford/weir/protocols/river"
	"github.com/psanford/weir/protocols/wl"
	"github.com/psanford/weir/wire"
)

// ErrUnavailable is returned by Run when the compositor refuses to grant
// window management to this client (usually because another window manager
// is already running).
var ErrUnavailable = errors.New("window management unavailable: is another window manager running?")

// Bridge connects a core.Model to a river compositor.
type Bridge struct {
	conn  *wire.Conn
	model *core.Model
	log   *slog.Logger

	registry *wire.Registry
	wm       *river.WindowManagerV1
	seat     *river.SeatV1 // first seat; weir is single-seat for now

	// outputGlobals maps wl_output global names to their advertised
	// versions, so the bridge can bind them at a supported version when a
	// river_output_v1.wl_output event references one.
	outputGlobals map[uint32]uint32

	windows map[core.WindowID]*windowState
	outputs map[core.OutputID]*outputState

	nextWindowID core.WindowID
	nextOutputID core.OutputID

	// arrangement is computed at manage_start and reused for the render
	// sequences that follow it.
	arrangement core.Arrangement
	// lastOrder is the most recently applied stacking order, used to skip
	// redundant restacking.
	lastOrder []core.WindowID
	// lastFocus is the window most recently given keyboard focus.
	lastFocus core.WindowID
	// focusSent records whether focus has ever been explicitly set.
	focusSent bool

	// Unavailable is set if the compositor sent the unavailable event.
	unavailable bool
}

// windowState is the bridge's per-window protocol bookkeeping: the proxies
// and the last state sent to the compositor, for diffing.
type windowState struct {
	id    core.WindowID
	proxy *river.WindowV1
	node  *river.NodeV1

	closed bool

	// Window-management state last sent (manage sequence).
	proposed             bool
	proposedW, proposedH int32
	tiled                core.Edges
	tiledSent            bool
	fullscreen           core.OutputID
	capsSent             bool
	ssd                  bool
	ssdSent              bool
	decorationHint       river.WindowV1DecorationHint

	// Rendering state last sent (render sequence).
	posSent    bool
	posX, posY int32
	hidden     bool
	border     core.Border
	borderSent bool
}

// outputState is the bridge's per-output protocol bookkeeping.
type outputState struct {
	id    core.OutputID
	proxy *river.OutputV1
	wlOut *wl.Output

	name    string
	rect    core.Rect
	hasPos  bool
	hasDims bool
	added   bool // OutputAdded has been delivered to the model
}

// New creates a bridge over an established Wayland connection.
func New(conn *wire.Conn, model *core.Model, logger *slog.Logger) *Bridge {
	if logger == nil {
		logger = slog.Default()
	}
	return &Bridge{
		conn:          conn,
		model:         model,
		log:           logger,
		windows:       make(map[core.WindowID]*windowState),
		outputs:       make(map[core.OutputID]*outputState),
		outputGlobals: make(map[uint32]uint32),
	}
}

// Bootstrap binds the river_window_manager_v1 global and waits for the
// compositor to either grant window management or declare it unavailable.
// It must be called before Run and before starting the connection's read
// loop (it performs synchronous round trips).
func (b *Bridge) Bootstrap() error {
	b.registry = b.conn.Display.GetRegistry()
	var wmName, wmVersion uint32
	found := false
	b.registry.OnGlobal = func(name uint32, iface string, version uint32) {
		switch iface {
		case river.WindowManagerV1Name:
			wmName, wmVersion = name, version
			found = true
		case wl.OutputName:
			b.outputGlobals[name] = version
		}
	}
	b.registry.OnGlobalRemove = func(name uint32) {
		delete(b.outputGlobals, name)
	}
	if err := b.conn.RoundTrip(); err != nil {
		return err
	}
	if !found {
		return errors.New("compositor does not advertise river_window_manager_v1 (is this river >= 0.4?)")
	}
	if wmVersion > river.WindowManagerV1Version {
		wmVersion = river.WindowManagerV1Version
	}
	b.wm = river.BindWindowManagerV1(b.registry, wmName, wmVersion)
	b.installWMHandlers()
	// A round trip guarantees we see unavailable (if it is coming) before
	// we start doing real work: the protocol promises unavailable is the
	// first and only event if it is sent at all.
	if err := b.conn.RoundTrip(); err != nil {
		return err
	}
	if b.unavailable {
		return ErrUnavailable
	}
	return nil
}

// Model returns the model the bridge drives. The model must only be
// accessed from the goroutine that runs the bridge.
func (b *Bridge) Model() *core.Model { return b.model }

// Dirty asks the compositor to start a manage sequence so that model
// changes made outside the protocol loop (e.g. by an IPC command) get
// applied. Safe to call in any phase.
func (b *Bridge) Dirty() {
	b.wm.ManageDirty()
}

// Flush writes buffered requests to the compositor.
func (b *Bridge) Flush() error { return b.conn.Flush() }

// installWMHandlers wires the window manager global's events to the model.
func (b *Bridge) installWMHandlers() {
	b.wm.OnUnavailable = func() {
		b.unavailable = true
	}
	b.wm.OnFinished = func() {
		// The compositor is done with us; Run's dispatch loop will
		// surface the connection close.
		b.log.Info("compositor finished with window manager")
	}
	b.wm.OnWindow = func(w *river.WindowV1) { b.addWindow(w) }
	b.wm.OnOutput = func(o *river.OutputV1) { b.addOutput(o) }
	b.wm.OnSeat = func(s *river.SeatV1) { b.addSeat(s) }
	b.wm.OnManageStart = func() { b.manage() }
	b.wm.OnRenderStart = func() { b.render() }
	b.wm.OnSessionLocked = func() { b.log.Debug("session locked") }
	b.wm.OnSessionUnlocked = func() { b.log.Debug("session unlocked") }
}

// ---------------------------------------------------------------------------
// Windows
// ---------------------------------------------------------------------------

func (b *Bridge) addWindow(w *river.WindowV1) {
	b.nextWindowID++
	ws := &windowState{id: b.nextWindowID, proxy: w}
	b.windows[ws.id] = ws
	b.model.WindowAdded(ws.id)
	b.log.Debug("window added", "id", ws.id)

	w.OnClosed = func() {
		b.log.Debug("window closed", "id", ws.id)
		ws.closed = true
		b.model.WindowClosed(ws.id)
		delete(b.windows, ws.id)
		if ws.node != nil {
			ws.node.Destroy()
			ws.node = nil
		}
		w.Destroy()
	}
	w.OnAppId = func(appID string) { b.model.WindowAppID(ws.id, appID) }
	w.OnTitle = func(title string) { b.model.WindowTitle(ws.id, title) }
	w.OnParent = func(parent *river.WindowV1) {
		b.model.WindowParent(ws.id, b.idForProxy(parent))
	}
	w.OnDimensionsHint = func(minW, minH, maxW, maxH int32) {
		b.model.WindowDimensionsHint(ws.id, minW, minH, maxW, maxH)
	}
	w.OnDimensions = func(width, height int32) {
		b.model.WindowDimensions(ws.id, width, height)
	}
	w.OnDecorationHint = func(hint river.WindowV1DecorationHint) {
		ws.decorationHint = hint
	}
	w.OnFullscreenRequested = func(out *river.OutputV1) {
		// Honor the request on the output the window is currently on (or
		// the requested output if the window gave one).
		target := core.OutputID(0)
		for id, os := range b.outputs {
			if os.proxy == out {
				target = id
			}
		}
		b.model.WindowFullscreenRequested(ws.id, target)
	}
	w.OnExitFullscreenRequested = func() {
		b.model.WindowExitFullscreenRequested(ws.id)
	}
	// Requests weir chooses to ignore entirely: maximize, minimize, window
	// menu, interactive move/resize (until M6).
}

// idForProxy returns the core ID for a window proxy, or 0.
func (b *Bridge) idForProxy(w *river.WindowV1) core.WindowID {
	if w == nil {
		return 0
	}
	for id, ws := range b.windows {
		if ws.proxy == w {
			return id
		}
	}
	return 0
}

// ---------------------------------------------------------------------------
// Outputs
// ---------------------------------------------------------------------------

func (b *Bridge) addOutput(o *river.OutputV1) {
	b.nextOutputID++
	os := &outputState{id: b.nextOutputID, proxy: o, name: fmt.Sprintf("output-%d", b.nextOutputID)}
	b.outputs[os.id] = os
	b.log.Debug("output added", "id", os.id)

	o.OnWlOutput = func(globalName uint32) {
		// Bind the corresponding wl_output to learn its name ("DP-1").
		// The name event requires wl_output version 4; on older
		// compositors the synthetic name is kept.
		version := b.outputGlobals[globalName]
		if version > wl.OutputVersion {
			version = wl.OutputVersion
		}
		if version < 4 {
			return
		}
		os.wlOut = wl.BindOutput(b.registry, globalName, version)
		os.wlOut.OnName = func(name string) {
			os.name = name
			if os.added {
				b.model.OutputRenamed(os.id, name)
			}
		}
	}
	o.OnPosition = func(x, y int32) {
		os.rect.X, os.rect.Y = x, y
		os.hasPos = true
		b.maybeAddOutput(os)
	}
	o.OnDimensions = func(w, h int32) {
		os.rect.W, os.rect.H = w, h
		os.hasDims = true
		b.maybeAddOutput(os)
	}
	o.OnRemoved = func() {
		b.log.Debug("output removed", "id", os.id)
		if os.added {
			b.model.OutputRemoved(os.id)
		}
		delete(b.outputs, os.id)
		if os.wlOut != nil {
			os.wlOut.Release()
			os.wlOut = nil
		}
		o.Destroy()
	}
}

// maybeAddOutput delivers OutputAdded to the model once both position and
// dimensions are known, and geometry updates thereafter.
func (b *Bridge) maybeAddOutput(os *outputState) {
	if !os.hasPos || !os.hasDims {
		return
	}
	if !os.added {
		os.added = true
		b.model.OutputAdded(os.id, os.name, os.rect)
		return
	}
	b.model.OutputGeometry(os.id, os.rect)
}

// ---------------------------------------------------------------------------
// Seats
// ---------------------------------------------------------------------------

func (b *Bridge) addSeat(s *river.SeatV1) {
	if b.seat != nil {
		// Multi-seat support is out of scope; track only the first seat.
		b.log.Warn("ignoring additional seat")
		s.OnRemoved = func() { s.Destroy() }
		return
	}
	b.seat = s
	s.OnRemoved = func() {
		b.seat = nil
		s.Destroy()
	}
	s.OnWindowInteraction = func(w *river.WindowV1) {
		if id := b.idForProxy(w); id != 0 {
			b.model.WindowInteracted(id)
		}
	}
	s.OnPointerEnter = func(w *river.WindowV1) {
		// Focus-follows-mouse is a policy decision for the model; for now
		// weir focuses on click/interaction only.
	}
}

// ---------------------------------------------------------------------------
// Manage sequence
// ---------------------------------------------------------------------------

// manage runs one manage sequence: apply all pending model decisions as
// window-management state and finish the sequence.
//
// Window-management state is only ever sent from inside this function, and
// this function is only called in response to a manage_start event, so the
// protocol's sequencing rules hold by construction.
func (b *Bridge) manage() {
	// Drain queued close requests.
	for _, id := range b.model.CloseRequests {
		if ws, ok := b.windows[id]; ok && !ws.closed {
			ws.proxy.Close()
		}
	}
	b.model.CloseRequests = b.model.CloseRequests[:0]

	b.arrangement = b.model.Arrange()
	b.model.ClearChanged()

	for id, ws := range b.windows {
		p, ok := b.arrangement.Placements[id]
		if !ok {
			continue
		}
		b.applyWindowManageState(ws, p)
	}

	// Keyboard focus.
	if b.seat != nil {
		focus := b.arrangement.Focus
		if focus != b.lastFocus || !b.focusSent {
			if ws, ok := b.windows[focus]; ok && focus != 0 {
				b.seat.FocusWindow(ws.proxy)
			} else {
				b.seat.ClearFocus()
			}
			b.lastFocus = focus
			b.focusSent = true
		}
	}

	b.wm.ManageFinish()
}

// applyWindowManageState sends the window-management half of a placement:
// proposed dimensions, tiled edges, fullscreen, capabilities, decorations.
func (b *Bridge) applyWindowManageState(ws *windowState, p core.Placement) {
	if !ws.capsSent {
		ws.proxy.SetCapabilities(river.WindowV1CapabilitiesFullscreen)
		ws.capsSent = true
	}

	// Decoration mode: ask for server-side decorations whenever the window
	// supports them so clients don't draw their own title bars inside a
	// tiled layout. Windows that only support CSD keep CSD.
	wantSSD := ws.decorationHint != river.WindowV1DecorationHintOnlySupportsCsd
	if !ws.ssdSent || wantSSD != ws.ssd {
		if wantSSD {
			ws.proxy.UseSsd()
		} else {
			ws.proxy.UseCsd()
		}
		ws.ssd = wantSSD
		ws.ssdSent = true
	}

	// Fullscreen transitions.
	if p.Fullscreen != ws.fullscreen {
		if p.Fullscreen != 0 {
			if os, ok := b.outputs[p.Fullscreen]; ok {
				ws.proxy.Fullscreen(os.proxy)
				ws.proxy.InformFullscreen()
			}
		} else {
			ws.proxy.ExitFullscreen()
			ws.proxy.InformNotFullscreen()
			// Force a re-propose: dimensions are undefined after exiting
			// fullscreen.
			ws.proposed = false
		}
		ws.fullscreen = p.Fullscreen
	}

	// Proposed dimensions. Fullscreen windows are sized by the compositor.
	if p.Fullscreen == 0 {
		w, h := p.Rect.W, p.Rect.H
		if !ws.proposed || w != ws.proposedW || h != ws.proposedH {
			ws.proxy.ProposeDimensions(w, h)
			ws.proposed = true
			ws.proposedW, ws.proposedH = w, h
		}
	}

	// Tiled edges.
	if !ws.tiledSent || p.Tiled != ws.tiled {
		ws.proxy.SetTiled(river.WindowV1Edges(p.Tiled))
		ws.tiled = p.Tiled
		ws.tiledSent = true
	}
}

// ---------------------------------------------------------------------------
// Render sequence
// ---------------------------------------------------------------------------

// render runs one render sequence: apply positions, stacking, visibility,
// and borders, then finish the sequence.
//
// Rendering state is only ever sent from inside this function, and this
// function is only called in response to a render_start event, so the
// protocol's sequencing rules hold by construction.
func (b *Bridge) render() {
	b.applyRenderState()
	b.wm.RenderFinish()
}

// applyRenderState sends the rendering half of the current arrangement.
func (b *Bridge) applyRenderState() {
	arr := b.arrangement
	for id, ws := range b.windows {
		p, ok := arr.Placements[id]
		if !ok {
			continue
		}
		// Visibility.
		if !p.Visible && !ws.hidden {
			ws.proxy.Hide()
			ws.hidden = true
		} else if p.Visible && ws.hidden {
			ws.proxy.Show()
			ws.hidden = false
		}
		if !p.Visible {
			continue
		}
		// Position.
		if ws.node == nil {
			ws.node = ws.proxy.GetNode()
		}
		if !ws.posSent || p.Rect.X != ws.posX || p.Rect.Y != ws.posY {
			ws.node.SetPosition(p.Rect.X, p.Rect.Y)
			ws.posSent = true
			ws.posX, ws.posY = p.Rect.X, p.Rect.Y
		}
		// Borders.
		if !ws.borderSent || p.Border != ws.border {
			b.sendBorders(ws, p.Border)
			ws.border = p.Border
			ws.borderSent = true
		}
	}
	b.applyStacking(arr.Order)
}

// sendBorders converts a core.Border into a set_borders request. River
// takes 32-bit-per-channel premultiplied RGBA.
func (b *Bridge) sendBorders(ws *windowState, border core.Border) {
	if border.Width <= 0 {
		ws.proxy.SetBorders(river.WindowV1EdgesNone, 0, 0, 0, 0, 0)
		return
	}
	r, g, bl, a := expandColor(border.Color)
	ws.proxy.SetBorders(
		river.WindowV1Edges(core.EdgeAll),
		border.Width,
		r, g, bl, a,
	)
}

// expandColor converts 0xRRGGBBAA to four 32-bit premultiplied channels.
func expandColor(c uint32) (r, g, b, a uint32) {
	expand := func(v uint32) uint32 { return v * 0x01010101 }
	r8, g8, b8, a8 := c>>24&0xff, c>>16&0xff, c>>8&0xff, c&0xff
	pre := func(v uint32) uint32 {
		return uint32(uint64(expand(v)) * uint64(a8) / 0xff)
	}
	return pre(r8), pre(g8), pre(b8), expand(a8)
}

// applyStacking realizes the bottom-to-top order with place_bottom and
// place_above requests. Skipped entirely if the order is unchanged.
func (b *Bridge) applyStacking(order []core.WindowID) {
	if equalOrder(order, b.lastOrder) {
		return
	}
	var prev *river.NodeV1
	for _, id := range order {
		ws, ok := b.windows[id]
		if !ok || ws.node == nil {
			continue
		}
		if prev == nil {
			ws.node.PlaceBottom()
		} else {
			ws.node.PlaceAbove(prev)
		}
		prev = ws.node
	}
	b.lastOrder = append(b.lastOrder[:0], order...)
}

func equalOrder(a, b []core.WindowID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Event loop
// ---------------------------------------------------------------------------

// Run processes compositor events until the connection fails or commands
// arrives on cmds. Each command is dispatched to the model; if the model
// changes as a result, a manage sequence is requested. The reply channel of
// each command receives the command's output or error.
func (b *Bridge) Run(cmds <-chan Command) error {
	packets := make(chan wire.Packet, 8)
	go b.conn.ReadLoop(packets)

	if err := b.conn.Flush(); err != nil {
		return err
	}
	for {
		select {
		case p := <-packets:
			if err := b.conn.Feed(p); err != nil {
				return err
			}
			if _, err := b.conn.DispatchPending(); err != nil {
				return err
			}
		case cmd := <-cmds:
			out, err := b.model.Dispatch(cmd.Args)
			cmd.Reply <- CommandResult{Output: out, Err: err}
			if b.model.Changed() {
				b.Dirty()
			}
		}
		if b.unavailable {
			return ErrUnavailable
		}
		if err := b.conn.Flush(); err != nil {
			return err
		}
	}
}

// Command is a request to run a core command on the bridge's goroutine.
type Command struct {
	Args  []string
	Reply chan CommandResult
}

// CommandResult is the outcome of a Command.
type CommandResult struct {
	Output string
	Err    error
}
