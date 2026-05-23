package core

import (
	"fmt"
	"sort"
)

// WindowID identifies a window. IDs are assigned by the caller (the bridge
// uses a counter; tests use whatever they like) and are never reused.
type WindowID uint64

// OutputID identifies an output. Same assignment rules as WindowID.
type OutputID uint64

// SeatID identifies a seat. weir currently assumes a single seat but keeps
// the ID in the model so that assumption is confined to the bridge.
type SeatID uint64

// WorkspaceMode controls how user-facing workspace names map to internal
// workspaces across multiple outputs. See ResolveWorkspace.
type WorkspaceMode string

const (
	// ModeIndependent: workspaces are global. Any output can view any
	// workspace; switching the workspace on one output does not affect
	// others. This is the xmonad/dwm model.
	ModeIndependent WorkspaceMode = "independent"
	// ModeLocked: a user-facing "desktop" expands to one internal workspace
	// per output. Switching desktops retargets every output atomically.
	ModeLocked WorkspaceMode = "locked"
)

// Window is the model's view of a single window.
type Window struct {
	ID     WindowID
	AppID  string
	Title  string
	Parent WindowID // 0 = no parent

	// Workspace is the internal name of the workspace this window belongs
	// to. Always set for a live window.
	Workspace string

	// Floating windows are excluded from tiling and rendered above tiled
	// windows at FloatRect.
	Floating  bool
	FloatRect Rect

	// FullscreenOn is the output the window is fullscreen on, or 0.
	FullscreenOn OutputID

	// DecorationOverride forces server-side ("ssd") or client-side ("csd")
	// decorations regardless of the client's preference. Empty means the
	// bridge decides from the client's decoration hint. Set by window
	// rules.
	DecorationOverride string

	// Dimension hints from the client. 0 = no preference.
	MinW, MinH, MaxW, MaxH int32

	// ActualW/H are the most recent dimensions reported by the compositor.
	// Zero until the first dimensions event arrives.
	ActualW, ActualH int32

	// pendingFloatCenter marks a floating window that has not yet been
	// positioned: once its actual dimensions arrive it is centered on its
	// output. This makes self-sizing dialogs appear centered.
	pendingFloatCenter bool
}

// Workspace is an ordered stack of windows plus its layout configuration.
// Index 0 of Windows is the first main window ("the master").
type Workspace struct {
	Name    string
	Windows []WindowID
	// Focus is the index into Windows of the focused window, or -1 if the
	// workspace is empty.
	Focus  int
	Layout LayoutName
	Params LayoutParams
}

// Output is a logical output (monitor).
type Output struct {
	ID   OutputID
	Name string
	Rect Rect
	// usable is the area remaining after layer shell surfaces (bars, docks)
	// reserve their exclusive zones. Zero means the full Rect is usable.
	// Tiled and floating windows are arranged within the usable area;
	// fullscreen windows cover the full Rect.
	usable Rect
	// Workspace is the internal name of the workspace shown on this output.
	Workspace string
}

// Usable returns the area of the output that windows should be arranged
// within: the output geometry minus any layer shell exclusive zones.
func (o *Output) Usable() Rect {
	if o.usable.Empty() {
		return o.Rect
	}
	return o.usable
}

// Model is the complete window-management state. It is a plain value-ish
// struct manipulated by event methods and commands; it performs no I/O.
//
// Model is not safe for concurrent use. The bridge owns it and serializes
// access (protocol events, IPC commands, and key bindings all funnel
// through one goroutine).
type Model struct {
	Windows    map[WindowID]*Window
	Workspaces map[string]*Workspace
	Outputs    map[OutputID]*Output

	// outputOrder preserves the order outputs were added in, for
	// next/prev navigation and deterministic iteration.
	outputOrder []OutputID

	// FocusedOutput is the output that has keyboard focus. 0 if there are
	// no outputs.
	FocusedOutput OutputID

	Mode WorkspaceMode

	// DefaultWorkspaces are the user-facing workspace names that exist from
	// startup. More are created on demand by view/send.
	DefaultWorkspaces []string

	// lastShown remembers which workspace each output (by name) was
	// showing when it was removed, so re-plugging a monitor restores its
	// workspace rather than assigning an arbitrary hidden one.
	lastShown map[string]string

	// Settings that apply to new windows / all windows.
	Borders BorderConfig

	// FocusFollowsCursor moves keyboard focus to the window under the
	// pointer as it moves, rather than only on click.
	FocusFollowsCursor bool

	// XcursorTheme and XcursorSize configure the pointer cursor. Empty
	// theme means the compositor default.
	XcursorTheme string
	XcursorSize  uint32

	// CloseRequests is the list of windows the user has asked to close.
	// The bridge drains this during the next manage sequence by sending
	// river_window_v1.close to each. Closing is a request to the client,
	// not a state change — the window stays in the model until the
	// compositor reports it closed.
	CloseRequests []WindowID

	// ExitRequested is set by the exit command. The bridge responds by
	// asking the compositor to end the Wayland session.
	ExitRequested bool

	// SpawnRequests is the list of shell commands queued by the spawn
	// command. The bridge drains and executes them after each command
	// dispatch.
	SpawnRequests []string

	// Bindings are the key bindings, keyed by chord. The bridge syncs
	// this set to river_xkb_binding_v1 protocol objects.
	Bindings map[bindingKey]Binding

	// PointerBindings are the pointer-button bindings, keyed by chord.
	PointerBindings map[pointerBindingKey]PointerBinding

	// Rules are the window rules, applied in order to new windows.
	Rules []Rule

	// InputDevices are the input devices the compositor has reported.
	InputDevices map[InputDeviceID]*InputDevice

	// KeyboardLayouts are the desired xkb keymaps. For a given keyboard
	// the last entry whose device glob matches wins.
	KeyboardLayouts []KeyboardLayout

	// InputSettings are the desired libinput device properties. For a
	// given device and property the last matching entry wins.
	InputSettings []InputSetting

	// op is the interactive pointer move/resize in progress, or nil.
	op *pointerOp

	// changed is set by any mutation and consumed by the bridge to decide
	// whether a manage_dirty request is needed.
	changed bool
}

// BorderConfig holds border appearance settings. River draws borders
// compositor-side; weir only picks widths and colors.
type BorderConfig struct {
	Width          int32
	FocusedColor   uint32 // 0xRRGGBBAA
	UnfocusedColor uint32
	UrgentColor    uint32
	SmartBorders   bool // hide borders when only one window is tiled
}

// DefaultBorders returns the border settings new models start with.
func DefaultBorders() BorderConfig {
	return BorderConfig{
		Width:          2,
		FocusedColor:   0x93a1a1ff,
		UnfocusedColor: 0x586e75ff,
		UrgentColor:    0xdc322fff,
		SmartBorders:   false,
	}
}

// NewModel returns an empty model with default settings.
func NewModel() *Model {
	names := make([]string, 0, 9)
	for i := 1; i <= 9; i++ {
		names = append(names, fmt.Sprintf("%d", i))
	}
	return &Model{
		Windows:           make(map[WindowID]*Window),
		Workspaces:        make(map[string]*Workspace),
		Outputs:           make(map[OutputID]*Output),
		Mode:              ModeIndependent,
		DefaultWorkspaces: names,
		Borders:           DefaultBorders(),
		Bindings:          make(map[bindingKey]Binding),
		PointerBindings:   make(map[pointerBindingKey]PointerBinding),
		InputDevices:      make(map[InputDeviceID]*InputDevice),
		lastShown:         make(map[string]string),
	}
}

// Changed reports whether the model has been mutated since the last call to
// ClearChanged. The bridge uses this to decide whether to request a manage
// sequence after handling an IPC command.
func (m *Model) Changed() bool { return m.changed }

// ClearChanged resets the changed flag.
func (m *Model) ClearChanged() { m.changed = false }

func (m *Model) markChanged() { m.changed = true }

// ---------------------------------------------------------------------------
// Workspace helpers
// ---------------------------------------------------------------------------

// ensureWorkspace returns the workspace with the given internal name,
// creating it if necessary.
func (m *Model) ensureWorkspace(name string) *Workspace {
	if ws, ok := m.Workspaces[name]; ok {
		return ws
	}
	ws := &Workspace{
		Name:   name,
		Focus:  -1,
		Layout: LayoutTile,
		Params: DefaultLayoutParams(),
	}
	m.Workspaces[name] = ws
	return ws
}

// ResolveWorkspace maps a user-facing workspace name to an internal
// workspace name for the given output, according to the current mode.
func (m *Model) ResolveWorkspace(userName string, output OutputID) string {
	if m.Mode == ModeLocked {
		if out, ok := m.Outputs[output]; ok {
			return userName + "@" + out.Name
		}
	}
	return userName
}

// workspaceVisibleOn returns the output currently showing the workspace, or
// 0 if it is hidden.
func (m *Model) workspaceVisibleOn(name string) OutputID {
	for _, id := range m.outputOrder {
		if m.Outputs[id].Workspace == name {
			return id
		}
	}
	return 0
}

// sortedWorkspaceNames returns all workspace names in a stable order.
func (m *Model) sortedWorkspaceNames() []string {
	names := make([]string, 0, len(m.Workspaces))
	for name := range m.Workspaces {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// nextHiddenWorkspace picks a workspace not visible on any output, for a
// newly added output to display. Prefers default workspaces in order, then
// any other hidden workspace, then invents a new default-style name.
func (m *Model) nextHiddenWorkspace(output OutputID) string {
	for _, userName := range m.DefaultWorkspaces {
		name := m.ResolveWorkspace(userName, output)
		m.ensureWorkspace(name)
		if m.workspaceVisibleOn(name) == 0 {
			return name
		}
	}
	for _, name := range m.sortedWorkspaceNames() {
		if m.workspaceVisibleOn(name) == 0 {
			return name
		}
	}
	// Every workspace is visible somewhere; invent a new one.
	for i := len(m.DefaultWorkspaces) + 1; ; i++ {
		name := m.ResolveWorkspace(fmt.Sprintf("%d", i), output)
		if _, exists := m.Workspaces[name]; !exists {
			m.ensureWorkspace(name)
			return name
		}
	}
}

// removeFromWorkspace removes a window from its workspace's stack, fixing up
// the focus index.
func (m *Model) removeFromWorkspace(w *Window) {
	ws, ok := m.Workspaces[w.Workspace]
	if !ok {
		return
	}
	idx := -1
	for i, id := range ws.Windows {
		if id == w.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	ws.Windows = append(ws.Windows[:idx], ws.Windows[idx+1:]...)
	switch {
	case len(ws.Windows) == 0:
		ws.Focus = -1
	case ws.Focus > idx:
		ws.Focus--
	case ws.Focus == idx:
		// Focus the window that took the removed window's place, or the
		// new last window if we removed the last one.
		if ws.Focus >= len(ws.Windows) {
			ws.Focus = len(ws.Windows) - 1
		}
	}
}

// ---------------------------------------------------------------------------
// Output events
// ---------------------------------------------------------------------------

// OutputAdded records a new output. The output shows the workspace it was
// showing when an output of the same name was last removed, if that
// workspace is still hidden; otherwise it shows the next hidden workspace
// (creating one if necessary).
func (m *Model) OutputAdded(id OutputID, name string, rect Rect) {
	if _, exists := m.Outputs[id]; exists {
		return
	}
	out := &Output{ID: id, Name: name, Rect: rect}
	m.Outputs[id] = out
	m.outputOrder = append(m.outputOrder, id)
	if prev, ok := m.lastShown[name]; ok && m.workspaceVisibleOn(prev) == 0 {
		m.ensureWorkspace(prev)
		out.Workspace = prev
	} else {
		out.Workspace = m.nextHiddenWorkspace(id)
	}
	if m.FocusedOutput == 0 {
		m.FocusedOutput = id
	}
	m.markChanged()
}

// OutputRenamed updates an output's name once the compositor reports it
// (the name typically arrives one round trip after the output itself).
// In locked workspace mode the output's existing workspaces keep their old
// internal names; only workspaces resolved after the rename use the new one.
//
// Because the real name arrives after OutputAdded, workspace restoration
// for a re-plugged monitor happens here: if an output with this name was
// previously removed while showing a workspace that is still hidden, and
// this output is still showing the empty workspace it was auto-assigned,
// switch to the remembered one.
func (m *Model) OutputRenamed(id OutputID, name string) {
	out, ok := m.Outputs[id]
	if !ok || out.Name == name || name == "" {
		return
	}
	out.Name = name
	if prev, ok := m.lastShown[name]; ok && prev != out.Workspace && m.workspaceVisibleOn(prev) == 0 {
		if cur := m.Workspaces[out.Workspace]; cur != nil && len(cur.Windows) == 0 {
			m.ensureWorkspace(prev)
			out.Workspace = prev
		}
	}
	m.markChanged()
}

// OutputGeometry updates an output's position and dimensions.
func (m *Model) OutputGeometry(id OutputID, rect Rect) {
	out, ok := m.Outputs[id]
	if !ok {
		return
	}
	if out.Rect == rect {
		return
	}
	out.Rect = rect
	// The usable area is stale until the compositor reports a new one.
	out.usable = Rect{}
	m.markChanged()
}

// OutputUsableArea records the area of the output remaining after layer
// shell exclusive zones (bars, docks) are subtracted. The coordinates are
// global, matching the output position. An area that does not intersect
// the output (or is empty) resets to the full output being usable.
func (m *Model) OutputUsableArea(id OutputID, area Rect) {
	out, ok := m.Outputs[id]
	if !ok {
		return
	}
	if area.Empty() || !out.Rect.Overlaps(area) {
		area = Rect{}
	}
	if out.usable == area {
		return
	}
	out.usable = area
	m.markChanged()
}

// OutputRemoved removes an output. The workspace it was showing becomes
// hidden; windows are never lost. Focus moves to another output if one
// exists.
func (m *Model) OutputRemoved(id OutputID) {
	out, ok := m.Outputs[id]
	if !ok {
		return
	}
	m.lastShown[out.Name] = out.Workspace
	delete(m.Outputs, id)
	for i, oid := range m.outputOrder {
		if oid == id {
			m.outputOrder = append(m.outputOrder[:i], m.outputOrder[i+1:]...)
			break
		}
	}
	// Any window fullscreen on the removed output exits fullscreen (the
	// protocol does this implicitly server-side; mirror it in the model).
	for _, w := range m.Windows {
		if w.FullscreenOn == id {
			w.FullscreenOn = 0
		}
	}
	if m.FocusedOutput == id {
		m.FocusedOutput = 0
		if len(m.outputOrder) > 0 {
			m.FocusedOutput = m.outputOrder[0]
		}
	}
	m.markChanged()
}

// ---------------------------------------------------------------------------
// Window events
// ---------------------------------------------------------------------------

// WindowAdded records a new window. It is appended to the focused output's
// current workspace and receives focus there. xmonad inserts new windows at
// the top of the stack; we append instead so the master is stable, matching
// river-classic's default. `zoom` promotes a window to master.
func (m *Model) WindowAdded(id WindowID) {
	if _, exists := m.Windows[id]; exists {
		return
	}
	w := &Window{ID: id}
	m.Windows[id] = w

	wsName := m.currentWorkspaceName()
	ws := m.ensureWorkspace(wsName)
	w.Workspace = wsName
	ws.Windows = append(ws.Windows, id)
	ws.Focus = len(ws.Windows) - 1
	m.markChanged()
}

// currentWorkspaceName returns the internal name of the workspace on the
// focused output, or a fallback workspace if there are no outputs yet.
func (m *Model) currentWorkspaceName() string {
	if out, ok := m.Outputs[m.FocusedOutput]; ok {
		return out.Workspace
	}
	// No outputs: park windows on the first default workspace so they are
	// adopted as soon as an output appears.
	name := "1"
	if len(m.DefaultWorkspaces) > 0 {
		name = m.DefaultWorkspaces[0]
	}
	m.ensureWorkspace(name)
	return name
}

// WindowClosed removes a window from the model entirely.
func (m *Model) WindowClosed(id WindowID) {
	w, ok := m.Windows[id]
	if !ok {
		return
	}
	m.removeFromWorkspace(w)
	delete(m.Windows, id)
	// Clear dangling parent references.
	for _, other := range m.Windows {
		if other.Parent == id {
			other.Parent = 0
		}
	}
	m.markChanged()
}

// WindowAppID records the window's application ID.
func (m *Model) WindowAppID(id WindowID, appID string) {
	if w, ok := m.Windows[id]; ok && w.AppID != appID {
		w.AppID = appID
		m.applyRules(w)
		m.markChanged()
	}
}

// WindowTitle records the window's title.
func (m *Model) WindowTitle(id WindowID, title string) {
	if w, ok := m.Windows[id]; ok && w.Title != title {
		w.Title = title
		m.applyRules(w)
		m.markChanged()
	}
}

// WindowParent records the window's parent. A window that gains a parent and
// has not been explicitly tiled floats by default (it is most likely a
// dialog).
func (m *Model) WindowParent(id WindowID, parent WindowID) {
	w, ok := m.Windows[id]
	if !ok || w.Parent == parent {
		return
	}
	w.Parent = parent
	if parent != 0 && !w.Floating {
		m.setFloating(w, true)
	}
	// Rules run after the dialog auto-float so an explicit no-float rule
	// can override it.
	m.applyRules(w)
	m.markChanged()
}

// WindowDimensionsHint records the window's preferred min/max dimensions.
//
// A window that declares a fixed size in either dimension (min == max,
// non-zero) cannot be resized by the layout, so tiling it is pointless:
// it floats, like a dialog. Clients typically only learn their natural
// size (and therefore set min == max) after their first layout pass, which
// can be after the window has already been displayed once, so unlike
// rules this is not gated on the window never having been shown. Window
// rules run afterwards so an explicit no-float rule can still override it.
func (m *Model) WindowDimensionsHint(id WindowID, minW, minH, maxW, maxH int32) {
	w, ok := m.Windows[id]
	if !ok {
		return
	}
	w.MinW, w.MinH, w.MaxW, w.MaxH = minW, minH, maxW, maxH
	fixed := (minW != 0 && minW == maxW) || (minH != 0 && minH == maxH)
	if fixed && !w.Floating {
		m.setFloating(w, true)
	}
	m.applyRules(w)
	m.markChanged()
}

// WindowDimensions records the actual dimensions reported by the compositor.
// This is render-sequence state: it never triggers a manage sequence by
// itself, so it does not set the changed flag unless a deferred float
// centering needs to happen.
func (m *Model) WindowDimensions(id WindowID, width, height int32) {
	w, ok := m.Windows[id]
	if !ok {
		return
	}
	w.ActualW, w.ActualH = width, height
	if w.pendingFloatCenter && w.Floating {
		w.pendingFloatCenter = false
		area := m.outputAreaForWorkspace(w.Workspace)
		w.FloatRect = area.CenterIn(width, height)
	}
}

// outputAreaForWorkspace returns the usable rect of the output showing the
// given workspace, the focused output's usable rect if the workspace is
// hidden, or a fallback rect if there are no outputs. Used for placing
// floating windows.
func (m *Model) outputAreaForWorkspace(name string) Rect {
	if id := m.workspaceVisibleOn(name); id != 0 {
		return m.Outputs[id].Usable()
	}
	if out, ok := m.Outputs[m.FocusedOutput]; ok {
		return out.Usable()
	}
	return Rect{W: 1920, H: 1080}
}

// setFloating changes a window's floating state, picking an initial float
// rectangle when a window starts floating.
func (m *Model) setFloating(w *Window, floating bool) {
	if w.Floating == floating {
		return
	}
	w.Floating = floating
	if floating && w.FloatRect.Empty() {
		area := m.outputAreaForWorkspace(w.Workspace)
		if w.ActualW > 0 && w.ActualH > 0 {
			w.FloatRect = area.CenterIn(w.ActualW, w.ActualH)
		} else {
			// Dimensions unknown: let the window size itself, then center
			// it when the dimensions event arrives.
			w.FloatRect = area.CenterIn(area.W/2, area.H/2)
			w.pendingFloatCenter = true
		}
	}
	m.markChanged()
}

// WindowFullscreenRequested handles a window asking to be made fullscreen
// (e.g. a video player going fullscreen). Policy: honor it. The preferred
// output may be 0, in which case the output showing the window's workspace
// is used.
func (m *Model) WindowFullscreenRequested(id WindowID, preferred OutputID) {
	w, ok := m.Windows[id]
	if !ok {
		return
	}
	target := preferred
	if _, exists := m.Outputs[target]; !exists {
		target = 0
	}
	if target == 0 {
		target = m.workspaceVisibleOn(w.Workspace)
	}
	if target == 0 {
		target = m.FocusedOutput
	}
	if target == 0 {
		return
	}
	if w.FullscreenOn != target {
		w.FullscreenOn = target
		m.markChanged()
	}
}

// WindowExitFullscreenRequested handles a window asking to leave
// fullscreen. Policy: honor it.
func (m *Model) WindowExitFullscreenRequested(id WindowID) {
	w, ok := m.Windows[id]
	if !ok || w.FullscreenOn == 0 {
		return
	}
	w.FullscreenOn = 0
	m.markChanged()
}

// WindowInteracted records that the user clicked/touched a window. Policy:
// focus follows interaction.
func (m *Model) WindowInteracted(id WindowID) {
	w, ok := m.Windows[id]
	if !ok {
		return
	}
	m.focusWindow(w)
}

// PointerEntered records that the pointer moved into a window. Focus
// follows it only if the focus-follows-cursor policy is enabled.
func (m *Model) PointerEntered(id WindowID) {
	if !m.FocusFollowsCursor {
		return
	}
	m.WindowInteracted(id)
}

// focusWindow makes the given window the focused window of its workspace and
// focuses the output showing that workspace (if any).
func (m *Model) focusWindow(w *Window) {
	ws, ok := m.Workspaces[w.Workspace]
	if !ok {
		return
	}
	for i, id := range ws.Windows {
		if id == w.ID {
			if ws.Focus != i {
				ws.Focus = i
				m.markChanged()
			}
			break
		}
	}
	if out := m.workspaceVisibleOn(w.Workspace); out != 0 && out != m.FocusedOutput {
		m.FocusedOutput = out
		m.markChanged()
	}
}

// FocusedWindow returns the window that should have keyboard focus, or nil.
func (m *Model) FocusedWindow() *Window {
	out, ok := m.Outputs[m.FocusedOutput]
	if !ok {
		return nil
	}
	ws, ok := m.Workspaces[out.Workspace]
	if !ok || ws.Focus < 0 || ws.Focus >= len(ws.Windows) {
		return nil
	}
	return m.Windows[ws.Windows[ws.Focus]]
}
