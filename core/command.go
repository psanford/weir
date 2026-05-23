package core

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// CmdError is returned for user errors (bad arguments, unknown commands).
// The IPC layer reports these to the client without logging them as
// internal failures.
type CmdError struct{ msg string }

func (e *CmdError) Error() string { return e.msg }

func cmdErrf(format string, args ...any) error {
	return &CmdError{msg: fmt.Sprintf(format, args...)}
}

// command describes one entry in the command table.
type command struct {
	name    string
	usage   string
	summary string
	run     func(m *Model, args []string) (string, error)
}

// commands is the single table of every action weir can perform.
// Keybindings, pointer bindings, the IPC socket, and tests all dispatch
// through it. Output (if any) is a string; mutating commands return "".
var commands []command

func init() {
	commands = []command{
		{"focus", "focus next|prev|main", "shift focus within the workspace", cmdFocus},
		{"swap", "swap next|prev|main", "swap the focused window within the stack", cmdSwap},
		{"zoom", "zoom", "promote the focused window to main (or cycle if already main)", cmdZoom},
		{"close", "close", "ask the focused window to close", cmdClose},
		{"view", "view <workspace>|next|prev", "show a workspace on the focused output", cmdView},
		{"pull", "pull <workspace>", "bring a workspace to the focused output, swapping if visible elsewhere", cmdPull},
		{"send", "send <workspace>", "move the focused window to a workspace", cmdSend},
		{"focus-output", "focus-output next|prev|left|right|up|down|<name>", "focus another output", cmdFocusOutput},
		{"send-to-output", "send-to-output next|prev|left|right|up|down|<name>", "move the focused window to another output", cmdSendToOutput},
		{"set-layout", "set-layout tile|monocle", "set the focused workspace's layout", cmdSetLayout},
		{"cycle-layout", "cycle-layout <l>[,<l>...]", "cycle the focused workspace through layouts (monocle|left|right|top|bottom)", cmdCycleLayout},
		{"set", "set <option> <value...>", "set a layout, appearance, or behavior option (run \"set\" alone to list them)", cmdSet},
		{"move", "move left|right|up|down <px>", "move the focused window (floating it if tiled)", cmdMove},
		{"snap", "snap left|right|up|down", "snap the focused window to an output edge (floating it if tiled)", cmdSnap},
		{"resize", "resize horizontal|vertical <px>", "grow or shrink the focused window (floating it if tiled)", cmdResize},
		{"toggle-float", "toggle-float", "toggle floating for the focused window", cmdToggleFloat},
		{"toggle-fullscreen", "toggle-fullscreen", "toggle fullscreen for the focused window", cmdToggleFullscreen},
		{"rule", "rule add|del [-app-id <glob>] [-title <glob>] <action> | rule list", "manage window rules (float|no-float|ssd|csd|workspace <ws>)", cmdRule},
		{"keyboard-layout", "keyboard-layout [-rules R] [-model M] [-variant V] [-options O] [-device <glob>] <layout>", "set the xkb keymap for matching keyboards", cmdKeyboardLayout},
		{"list-inputs", "list-inputs", "list input devices", cmdListInputs},
		{"workspace-mode", "workspace-mode independent|locked", "set how workspaces map to outputs", cmdWorkspaceMode},
		{"bind", "bind <mods+key> <command...>", "bind a key chord to a command", cmdBind},
		{"unbind", "unbind <mods+key>", "remove a key binding", cmdUnbind},
		{"bind-pointer", "bind-pointer <mods+button> move|resize|<command...>", "bind a pointer button", cmdBindPointer},
		{"unbind-pointer", "unbind-pointer <mods+button>", "remove a pointer binding", cmdUnbindPointer},
		{"list-bindings", "list-bindings", "list all key and pointer bindings", cmdListBindings},
		{"spawn", "spawn <command...>", "run a shell command", cmdSpawn},
		{"get", "get state|outputs|windows|workspaces", "query weir state as JSON", cmdGet},
		{"exit", "exit", "end the Wayland session", cmdExit},
		{"help", "help [command]", "list commands or show usage for one", cmdHelp},
	}
	sort.Slice(commands, func(i, j int) bool { return commands[i].name < commands[j].name })
}

// Dispatch parses and executes a command. The returned string is the
// command's output (empty for most mutating commands). Errors of type
// *CmdError are user errors; anything else is a bug.
func (m *Model) Dispatch(args []string) (string, error) {
	if len(args) == 0 {
		return "", cmdErrf("empty command")
	}
	for i := range commands {
		if commands[i].name == args[0] {
			return commands[i].run(m, args[1:])
		}
	}
	return "", cmdErrf("unknown command %q (try \"help\")", args[0])
}

// ---------------------------------------------------------------------------
// Focus and stack manipulation
// ---------------------------------------------------------------------------

// focusedWorkspace returns the workspace on the focused output, or nil.
func (m *Model) focusedWorkspace() *Workspace {
	out, ok := m.Outputs[m.FocusedOutput]
	if !ok {
		return nil
	}
	return m.Workspaces[out.Workspace]
}

func cmdFocus(m *Model, args []string) (string, error) {
	if len(args) != 1 {
		return "", cmdErrf("usage: focus next|prev|main")
	}
	ws := m.focusedWorkspace()
	if ws == nil || len(ws.Windows) == 0 {
		return "", nil
	}
	n := len(ws.Windows)
	switch args[0] {
	case "next":
		ws.Focus = (ws.Focus + 1) % n
	case "prev":
		ws.Focus = (ws.Focus - 1 + n) % n
	case "main":
		ws.Focus = 0
	default:
		return "", cmdErrf("usage: focus next|prev|main")
	}
	m.markChanged()
	return "", nil
}

func cmdSwap(m *Model, args []string) (string, error) {
	if len(args) != 1 {
		return "", cmdErrf("usage: swap next|prev|main")
	}
	ws := m.focusedWorkspace()
	if ws == nil || len(ws.Windows) < 2 || ws.Focus < 0 {
		return "", nil
	}
	n := len(ws.Windows)
	var target int
	switch args[0] {
	case "next":
		target = (ws.Focus + 1) % n
	case "prev":
		target = (ws.Focus - 1 + n) % n
	case "main":
		target = 0
	default:
		return "", cmdErrf("usage: swap next|prev|main")
	}
	ws.Windows[ws.Focus], ws.Windows[target] = ws.Windows[target], ws.Windows[ws.Focus]
	ws.Focus = target
	m.markChanged()
	return "", nil
}

// cmdZoom implements xmonad's promote: move the focused window to the main
// position. If it is already the main window, swap it with the second
// window so repeated zooms cycle the top of the stack.
func cmdZoom(m *Model, _ []string) (string, error) {
	ws := m.focusedWorkspace()
	if ws == nil || len(ws.Windows) < 2 || ws.Focus < 0 {
		return "", nil
	}
	if ws.Focus == 0 {
		ws.Windows[0], ws.Windows[1] = ws.Windows[1], ws.Windows[0]
		// Keep focus on the window we just demoted? xmonad keeps focus on
		// the promoted window. Focus follows the window that moved to main.
		ws.Focus = 0
	} else {
		id := ws.Windows[ws.Focus]
		copy(ws.Windows[1:ws.Focus+1], ws.Windows[0:ws.Focus])
		ws.Windows[0] = id
		ws.Focus = 0
	}
	m.markChanged()
	return "", nil
}

func cmdClose(m *Model, _ []string) (string, error) {
	w := m.FocusedWindow()
	if w == nil {
		return "", nil
	}
	m.CloseRequests = append(m.CloseRequests, w.ID)
	m.markChanged()
	return "", nil
}

// ---------------------------------------------------------------------------
// Workspaces
// ---------------------------------------------------------------------------

func cmdView(m *Model, args []string) (string, error) {
	if len(args) != 1 || args[0] == "" {
		return "", cmdErrf("usage: view <workspace>|next|prev")
	}
	switch args[0] {
	case "next":
		return "", m.viewRelative(1)
	case "prev":
		return "", m.viewRelative(-1)
	}
	return "", m.View(args[0], false)
}

func cmdPull(m *Model, args []string) (string, error) {
	if len(args) != 1 || args[0] == "" {
		return "", cmdErrf("usage: pull <workspace>")
	}
	return "", m.View(args[0], true)
}

// cycleWorkspaces returns the user-facing workspace names to cycle through
// with view next/prev: the default workspaces in their declared order,
// followed by any other non-empty workspaces in sorted order.
func (m *Model) cycleWorkspaces() []string {
	names := append([]string(nil), m.DefaultWorkspaces...)
	seen := make(map[string]bool, len(names))
	for _, n := range names {
		seen[n] = true
	}
	var extra []string
	for _, internal := range m.sortedWorkspaceNames() {
		ws := m.Workspaces[internal]
		if len(ws.Windows) == 0 {
			continue
		}
		user := userWorkspaceName(internal)
		if !seen[user] {
			seen[user] = true
			extra = append(extra, user)
		}
	}
	sort.Strings(extra)
	return append(names, extra...)
}

// userWorkspaceName strips the locked-mode "@output" suffix from an
// internal workspace name.
func userWorkspaceName(internal string) string {
	if i := strings.LastIndexByte(internal, '@'); i > 0 {
		return internal[:i]
	}
	return internal
}

// viewRelative advances the focused output to the next or previous
// workspace in the cycle list.
func (m *Model) viewRelative(dir int) error {
	out, ok := m.Outputs[m.FocusedOutput]
	if !ok {
		return cmdErrf("no outputs")
	}
	names := m.cycleWorkspaces()
	if len(names) == 0 {
		return nil
	}
	cur := userWorkspaceName(out.Workspace)
	idx := -1
	for i, n := range names {
		if n == cur {
			idx = i
			break
		}
	}
	// Starting outside the list lands on the first or last entry.
	next := 0
	if idx >= 0 {
		next = (idx + dir + len(names)) % len(names)
	} else if dir < 0 {
		next = len(names) - 1
	}
	return m.View(names[next], false)
}

// View shows the user-facing workspace name on the focused output.
//
// If the workspace is already visible on another output: with greedy=false
// (xmonad's view) focus simply moves to that output; with greedy=true
// (xmonad's greedyView) the two outputs swap workspaces so the requested
// one appears on the focused output.
//
// In locked mode every output switches to its own expansion of the name in
// the same call, which the bridge applies in one atomic manage sequence.
func (m *Model) View(userName string, greedy bool) error {
	if len(m.Outputs) == 0 {
		return cmdErrf("no outputs")
	}
	if m.Mode == ModeLocked {
		for _, outID := range m.outputOrder {
			name := m.ResolveWorkspace(userName, outID)
			m.ensureWorkspace(name)
			m.Outputs[outID].Workspace = name
		}
		m.markChanged()
		return nil
	}

	out := m.Outputs[m.FocusedOutput]
	name := m.ResolveWorkspace(userName, m.FocusedOutput)
	m.ensureWorkspace(name)
	if out.Workspace == name {
		return nil
	}
	if other := m.workspaceVisibleOn(name); other != 0 {
		if !greedy {
			m.FocusedOutput = other
			m.markChanged()
			return nil
		}
		m.Outputs[other].Workspace = out.Workspace
	}
	out.Workspace = name
	m.markChanged()
	return nil
}

func cmdSend(m *Model, args []string) (string, error) {
	if len(args) != 1 || args[0] == "" {
		return "", cmdErrf("usage: send <workspace>")
	}
	w := m.FocusedWindow()
	if w == nil {
		return "", nil
	}
	name := m.ResolveWorkspace(args[0], m.FocusedOutput)
	m.MoveWindowToWorkspace(w, name)
	return "", nil
}

// MoveWindowToWorkspace moves a window to the named internal workspace,
// appending it to the destination stack and focusing it there.
func (m *Model) MoveWindowToWorkspace(w *Window, name string) {
	if w.Workspace == name {
		return
	}
	m.removeFromWorkspace(w)
	dst := m.ensureWorkspace(name)
	w.Workspace = name
	dst.Windows = append(dst.Windows, w.ID)
	dst.Focus = len(dst.Windows) - 1
	m.markChanged()
}

// ---------------------------------------------------------------------------
// Outputs
// ---------------------------------------------------------------------------

func cmdFocusOutput(m *Model, args []string) (string, error) {
	if len(args) != 1 {
		return "", cmdErrf("usage: focus-output next|prev|left|right|up|down|<name>")
	}
	target := m.resolveOutput(args[0])
	if target == 0 {
		return "", nil
	}
	if target != m.FocusedOutput {
		m.FocusedOutput = target
		m.markChanged()
	}
	return "", nil
}

func cmdSendToOutput(m *Model, args []string) (string, error) {
	if len(args) != 1 {
		return "", cmdErrf("usage: send-to-output next|prev|left|right|up|down|<name>")
	}
	w := m.FocusedWindow()
	if w == nil {
		return "", nil
	}
	target := m.resolveOutput(args[0])
	if target == 0 || target == m.FocusedOutput {
		return "", nil
	}
	m.MoveWindowToWorkspace(w, m.Outputs[target].Workspace)
	return "", nil
}

// resolveOutput maps a direction or output name to an output ID. Returns 0
// if there is no match (e.g. no output to the left).
func (m *Model) resolveOutput(arg string) OutputID {
	if len(m.outputOrder) == 0 {
		return 0
	}
	cur := -1
	for i, id := range m.outputOrder {
		if id == m.FocusedOutput {
			cur = i
			break
		}
	}
	switch arg {
	case "next":
		if cur < 0 {
			return m.outputOrder[0]
		}
		return m.outputOrder[(cur+1)%len(m.outputOrder)]
	case "prev":
		if cur < 0 {
			return m.outputOrder[0]
		}
		return m.outputOrder[(cur-1+len(m.outputOrder))%len(m.outputOrder)]
	case "left", "right", "up", "down":
		return m.outputInDirection(arg)
	default:
		for _, id := range m.outputOrder {
			if m.Outputs[id].Name == arg {
				return id
			}
		}
		return 0
	}
}

// outputInDirection returns the nearest output whose center lies in the
// given direction from the focused output's center, or 0.
func (m *Model) outputInDirection(dir string) OutputID {
	cur, ok := m.Outputs[m.FocusedOutput]
	if !ok {
		return 0
	}
	cx, cy := cur.Rect.Center()
	var best OutputID
	var bestDist int64 = 1<<63 - 1
	for _, id := range m.outputOrder {
		if id == m.FocusedOutput {
			continue
		}
		ox, oy := m.Outputs[id].Rect.Center()
		dx, dy := int64(ox-cx), int64(oy-cy)
		var ahead bool
		switch dir {
		case "left":
			ahead = dx < 0
		case "right":
			ahead = dx > 0
		case "up":
			ahead = dy < 0
		case "down":
			ahead = dy > 0
		}
		if !ahead {
			continue
		}
		dist := dx*dx + dy*dy
		if dist < bestDist {
			bestDist = dist
			best = id
		}
	}
	return best
}

// ---------------------------------------------------------------------------
// Layout options
// ---------------------------------------------------------------------------

func cmdSetLayout(m *Model, args []string) (string, error) {
	if len(args) != 1 || !ValidLayout(LayoutName(args[0])) {
		return "", cmdErrf("usage: set-layout tile|monocle")
	}
	ws := m.focusedWorkspace()
	if ws == nil {
		return "", cmdErrf("no focused workspace")
	}
	if ws.Layout != LayoutName(args[0]) {
		ws.Layout = LayoutName(args[0])
		m.markChanged()
	}
	return "", nil
}

// cmdCycleLayout advances the focused workspace to the next entry in a
// user-supplied list of layout specs. A spec is either "monocle" or a main
// location ("left", "right", "top", "bottom", which imply the tile layout).
// The current position in the cycle is derived from the workspace's actual
// layout state, so the command is stateless and the cycle stays coherent
// even if the layout is changed by other means in between.
func cmdCycleLayout(m *Model, args []string) (string, error) {
	if len(args) != 1 {
		return "", cmdErrf("usage: cycle-layout <layout>[,<layout>...] (e.g. cycle-layout monocle,left,top)")
	}
	specs := strings.Split(args[0], ",")
	if len(specs) < 2 {
		return "", cmdErrf("cycle-layout needs at least two layouts to cycle between")
	}
	for _, s := range specs {
		if !validLayoutSpec(s) {
			return "", cmdErrf("invalid layout %q (want monocle|left|right|top|bottom)", s)
		}
	}
	ws := m.focusedWorkspace()
	if ws == nil {
		return "", cmdErrf("no focused workspace")
	}
	cur := currentLayoutSpec(ws)
	next := specs[0]
	for i, s := range specs {
		if s == cur {
			next = specs[(i+1)%len(specs)]
			break
		}
	}
	applyLayoutSpec(ws, next)
	m.markChanged()
	return "", nil
}

// validLayoutSpec reports whether s is a valid cycle-layout entry.
func validLayoutSpec(s string) bool {
	if s == "monocle" {
		return true
	}
	_, err := ParseMainLocation(s)
	return err == nil
}

// currentLayoutSpec returns the cycle-layout spec describing the
// workspace's current layout.
func currentLayoutSpec(ws *Workspace) string {
	if ws.Layout == LayoutMonocle {
		return "monocle"
	}
	return string(ws.Params.MainLocation)
}

// applyLayoutSpec sets the workspace's layout to match a cycle-layout spec.
// Switching to monocle preserves the main location so cycling back to a
// tiled spec restores it.
func applyLayoutSpec(ws *Workspace, spec string) {
	if spec == "monocle" {
		ws.Layout = LayoutMonocle
		return
	}
	loc, _ := ParseMainLocation(spec)
	ws.Layout = LayoutTile
	ws.Params.MainLocation = loc
}

// setUsage lists every option the set command accepts. Returned whenever
// set is invoked without an option or with an unknown one, so the full
// surface is discoverable from the command line.
const setUsage = `usage: set <option> <value...>

layout (per workspace):
  main-ratio <r|+d|-d>             main area fraction of the output (0.1-0.9)
  main-count <n|+d|-d>             number of windows in the main area
  main-location left|right|top|bottom
  gaps <inner> <outer>             pixels between windows / at output edges
  smart-gaps on|off                drop the gaps when only one window is tiled

appearance:
  border-width <px>
  border-color-focused 0xRRGGBB[AA]
  border-color-unfocused 0xRRGGBB[AA]
  border-color-urgent 0xRRGGBB[AA]
  smart-borders on|off             drop the borders when only one window is tiled
  xcursor-theme <name> [size]

behavior:
  focus-follows-cursor on|off`

func cmdSet(m *Model, args []string) (string, error) {
	if len(args) < 1 {
		return "", cmdErrf("%s", setUsage)
	}
	opt, vals := args[0], args[1:]
	ws := m.focusedWorkspace()
	switch opt {
	case "main-ratio":
		if len(vals) != 1 {
			return "", cmdErrf("usage: set main-ratio <ratio|+delta|-delta>")
		}
		if ws == nil {
			return "", cmdErrf("no focused workspace")
		}
		v, err := parseAdjustFloat(vals[0], ws.Params.MainRatio)
		if err != nil {
			return "", err
		}
		ws.Params.MainRatio = v
		ws.Params = ws.Params.Clamp()
	case "main-count":
		if len(vals) != 1 {
			return "", cmdErrf("usage: set main-count <n|+delta|-delta>")
		}
		if ws == nil {
			return "", cmdErrf("no focused workspace")
		}
		v, err := parseAdjustInt(vals[0], ws.Params.MainCount)
		if err != nil {
			return "", err
		}
		ws.Params.MainCount = v
		ws.Params = ws.Params.Clamp()
	case "main-location":
		if len(vals) != 1 {
			return "", cmdErrf("usage: set main-location left|right|top|bottom")
		}
		if ws == nil {
			return "", cmdErrf("no focused workspace")
		}
		loc, err := ParseMainLocation(vals[0])
		if err != nil {
			return "", cmdErrf("%v", err)
		}
		ws.Params.MainLocation = loc
	case "gaps":
		if len(vals) != 2 {
			return "", cmdErrf("usage: set gaps <inner> <outer>")
		}
		if ws == nil {
			return "", cmdErrf("no focused workspace")
		}
		inner, err1 := strconv.ParseInt(vals[0], 10, 32)
		outer, err2 := strconv.ParseInt(vals[1], 10, 32)
		if err1 != nil || err2 != nil || inner < 0 || outer < 0 {
			return "", cmdErrf("gaps must be non-negative integers")
		}
		ws.Params.InnerGap = int32(inner)
		ws.Params.OuterGap = int32(outer)
	case "smart-gaps":
		if len(vals) != 1 || (vals[0] != "on" && vals[0] != "off") {
			return "", cmdErrf("usage: set smart-gaps on|off")
		}
		if ws == nil {
			return "", cmdErrf("no focused workspace")
		}
		ws.Params.SmartGaps = vals[0] == "on"
	case "border-width":
		if len(vals) != 1 {
			return "", cmdErrf("usage: set border-width <px>")
		}
		n, err := strconv.ParseInt(vals[0], 10, 32)
		if err != nil || n < 0 {
			return "", cmdErrf("border width must be a non-negative integer")
		}
		m.Borders.Width = int32(n)
	case "border-color-focused", "border-color-unfocused", "border-color-urgent":
		if len(vals) != 1 {
			return "", cmdErrf("usage: set %s 0xRRGGBBAA", opt)
		}
		c, err := parseColor(vals[0])
		if err != nil {
			return "", err
		}
		switch opt {
		case "border-color-focused":
			m.Borders.FocusedColor = c
		case "border-color-unfocused":
			m.Borders.UnfocusedColor = c
		case "border-color-urgent":
			m.Borders.UrgentColor = c
		}
	case "smart-borders":
		if len(vals) != 1 || (vals[0] != "on" && vals[0] != "off") {
			return "", cmdErrf("usage: set smart-borders on|off")
		}
		m.Borders.SmartBorders = vals[0] == "on"
	case "focus-follows-cursor":
		if len(vals) != 1 || (vals[0] != "on" && vals[0] != "off") {
			return "", cmdErrf("usage: set focus-follows-cursor on|off")
		}
		m.FocusFollowsCursor = vals[0] == "on"
	case "xcursor-theme":
		if len(vals) < 1 || len(vals) > 2 {
			return "", cmdErrf("usage: set xcursor-theme <name> [size]")
		}
		size := int64(24)
		if len(vals) == 2 {
			var err error
			size, err = strconv.ParseInt(vals[1], 10, 32)
			if err != nil || size <= 0 {
				return "", cmdErrf("cursor size must be a positive integer")
			}
		}
		m.XcursorTheme = vals[0]
		m.XcursorSize = uint32(size)
	default:
		return "", cmdErrf("unknown option %q\n\n%s", opt, setUsage)
	}
	m.markChanged()
	return "", nil
}

// parseAdjustFloat parses either an absolute float or a +/- prefixed delta
// applied to cur.
func parseAdjustFloat(s string, cur float64) (float64, error) {
	if strings.HasPrefix(s, "+") || strings.HasPrefix(s, "-") {
		d, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, cmdErrf("invalid adjustment %q", s)
		}
		return cur + d, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, cmdErrf("invalid value %q", s)
	}
	return v, nil
}

// parseAdjustInt parses either an absolute int or a +/- prefixed delta.
func parseAdjustInt(s string, cur int) (int, error) {
	if strings.HasPrefix(s, "+") || strings.HasPrefix(s, "-") {
		d, err := strconv.Atoi(s)
		if err != nil {
			return 0, cmdErrf("invalid adjustment %q", s)
		}
		return cur + d, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, cmdErrf("invalid value %q", s)
	}
	return v, nil
}

// parseColor parses a 0xRRGGBBAA or RRGGBBAA color. A 6-digit value is
// treated as fully opaque.
func parseColor(s string) (uint32, error) {
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "#")
	switch len(s) {
	case 6:
		v, err := strconv.ParseUint(s, 16, 32)
		if err != nil {
			return 0, cmdErrf("invalid color %q", s)
		}
		return uint32(v)<<8 | 0xff, nil
	case 8:
		v, err := strconv.ParseUint(s, 16, 32)
		if err != nil {
			return 0, cmdErrf("invalid color %q", s)
		}
		return uint32(v), nil
	default:
		return 0, cmdErrf("invalid color %q (want RRGGBB or RRGGBBAA)", s)
	}
}

// ---------------------------------------------------------------------------
// Window state toggles
// ---------------------------------------------------------------------------

func cmdToggleFloat(m *Model, _ []string) (string, error) {
	w := m.FocusedWindow()
	if w == nil {
		return "", nil
	}
	m.setFloating(w, !w.Floating)
	return "", nil
}

func cmdToggleFullscreen(m *Model, _ []string) (string, error) {
	w := m.FocusedWindow()
	if w == nil {
		return "", nil
	}
	if w.FullscreenOn != 0 {
		w.FullscreenOn = 0
	} else {
		out := m.workspaceVisibleOn(w.Workspace)
		if out == 0 {
			out = m.FocusedOutput
		}
		if out == 0 {
			return "", cmdErrf("no output to fullscreen on")
		}
		w.FullscreenOn = out
	}
	m.markChanged()
	return "", nil
}

func cmdWorkspaceMode(m *Model, args []string) (string, error) {
	if len(args) != 1 {
		return "", cmdErrf("usage: workspace-mode independent|locked")
	}
	switch WorkspaceMode(args[0]) {
	case ModeIndependent, ModeLocked:
		if m.Mode != WorkspaceMode(args[0]) {
			m.Mode = WorkspaceMode(args[0])
			m.markChanged()
		}
		return "", nil
	}
	return "", cmdErrf("usage: workspace-mode independent|locked")
}

// ---------------------------------------------------------------------------
// Queries
// ---------------------------------------------------------------------------

func cmdGet(m *Model, args []string) (string, error) {
	if len(args) != 1 {
		return "", cmdErrf("usage: get state|outputs|windows|workspaces")
	}
	var v any
	switch args[0] {
	case "state":
		v = m.Snapshot()
	case "outputs":
		v = m.Snapshot().Outputs
	case "windows":
		v = m.Snapshot().Windows
	case "workspaces":
		v = m.Snapshot().Workspaces
	default:
		return "", cmdErrf("usage: get state|outputs|windows|workspaces")
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal state: %w", err)
	}
	return string(b), nil
}

func cmdExit(m *Model, _ []string) (string, error) {
	m.ExitRequested = true
	return "", nil
}

func cmdHelp(_ *Model, args []string) (string, error) {
	var b strings.Builder
	if len(args) == 1 {
		for i := range commands {
			if commands[i].name == args[0] {
				fmt.Fprintf(&b, "%s\n  %s\n", commands[i].usage, commands[i].summary)
				return b.String(), nil
			}
		}
		return "", cmdErrf("unknown command %q", args[0])
	}
	for i := range commands {
		fmt.Fprintf(&b, "%-24s %s\n", commands[i].name, commands[i].summary)
	}
	return b.String(), nil
}
