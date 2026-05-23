package core

import (
	"strings"
	"testing"
)

// run dispatches a command and fails the test on error.
func run(t *testing.T, m *Model, args ...string) string {
	t.Helper()
	out, err := m.Dispatch(args)
	if err != nil {
		t.Fatalf("Dispatch(%v): %v", args, err)
	}
	return out
}

// twoOutputs returns a model with DP-1 at the origin and HDMI-A-1 to its
// right.
func twoOutputs() *Model {
	m := NewModel()
	m.OutputAdded(1, "DP-1", Rect{X: 0, Y: 0, W: 1920, H: 1080})
	m.OutputAdded(2, "HDMI-A-1", Rect{X: 1920, Y: 0, W: 2560, H: 1440})
	return m
}

func TestOutputsGetDistinctWorkspaces(t *testing.T) {
	m := twoOutputs()
	if got := m.Outputs[1].Workspace; got != "1" {
		t.Errorf("DP-1 workspace = %q, want 1", got)
	}
	if got := m.Outputs[2].Workspace; got != "2" {
		t.Errorf("HDMI-A-1 workspace = %q, want 2", got)
	}
	if m.FocusedOutput != 1 {
		t.Errorf("focused output = %d, want 1 (first added)", m.FocusedOutput)
	}
}

func TestWindowAddedJoinsFocusedWorkspaceAndGetsFocus(t *testing.T) {
	m := twoOutputs()
	m.WindowAdded(10)
	m.WindowAdded(11)
	ws := m.Workspaces["1"]
	if len(ws.Windows) != 2 || ws.Windows[0] != 10 || ws.Windows[1] != 11 {
		t.Fatalf("workspace 1 windows = %v", ws.Windows)
	}
	if ws.Focus != 1 {
		t.Errorf("focus = %d, want 1 (newest window)", ws.Focus)
	}
	if fw := m.FocusedWindow(); fw == nil || fw.ID != 11 {
		t.Errorf("FocusedWindow = %v, want 11", fw)
	}
}

func TestWindowClosedFixesFocus(t *testing.T) {
	m := twoOutputs()
	for id := WindowID(10); id <= 12; id++ {
		m.WindowAdded(id)
	}
	ws := m.Workspaces["1"]
	ws.Focus = 1 // focus the middle window

	m.WindowClosed(11)
	if len(ws.Windows) != 2 || ws.Windows[0] != 10 || ws.Windows[1] != 12 {
		t.Fatalf("windows = %v", ws.Windows)
	}
	if ws.Focus != 1 {
		t.Errorf("focus = %d, want 1 (window that took the closed slot)", ws.Focus)
	}

	m.WindowClosed(12)
	if ws.Focus != 0 {
		t.Errorf("focus = %d, want 0", ws.Focus)
	}
	m.WindowClosed(10)
	if ws.Focus != -1 {
		t.Errorf("focus = %d, want -1 for empty workspace", ws.Focus)
	}
}

func TestFocusNextPrevWraps(t *testing.T) {
	m := twoOutputs()
	for id := WindowID(10); id <= 12; id++ {
		m.WindowAdded(id)
	}
	ws := m.Workspaces["1"]
	if ws.Focus != 2 {
		t.Fatalf("initial focus = %d", ws.Focus)
	}
	run(t, m, "focus", "next")
	if ws.Focus != 0 {
		t.Errorf("focus next from last = %d, want 0 (wrap)", ws.Focus)
	}
	run(t, m, "focus", "prev")
	if ws.Focus != 2 {
		t.Errorf("focus prev from first = %d, want 2 (wrap)", ws.Focus)
	}
	run(t, m, "focus", "main")
	if ws.Focus != 0 {
		t.Errorf("focus main = %d, want 0", ws.Focus)
	}
}

func TestSwapAndZoom(t *testing.T) {
	m := twoOutputs()
	for id := WindowID(10); id <= 12; id++ {
		m.WindowAdded(id)
	}
	ws := m.Workspaces["1"]

	// Stack is [10 11 12], focus on 12. Swap prev -> [10 12 11], focus follows 12.
	run(t, m, "swap", "prev")
	if ws.Windows[1] != 12 || ws.Windows[2] != 11 || ws.Focus != 1 {
		t.Fatalf("after swap prev: windows=%v focus=%d", ws.Windows, ws.Focus)
	}

	// Zoom promotes 12 to main: [12 10 11].
	run(t, m, "zoom")
	if ws.Windows[0] != 12 || ws.Windows[1] != 10 || ws.Windows[2] != 11 {
		t.Fatalf("after zoom: windows=%v", ws.Windows)
	}
	if ws.Focus != 0 {
		t.Errorf("after zoom: focus=%d, want 0", ws.Focus)
	}

	// Zoom again while main swaps with the second window: [10 12 11].
	run(t, m, "zoom")
	if ws.Windows[0] != 10 || ws.Windows[1] != 12 {
		t.Fatalf("after second zoom: windows=%v", ws.Windows)
	}
}

func TestViewSwitchesWorkspace(t *testing.T) {
	m := twoOutputs()
	m.WindowAdded(10)
	run(t, m, "view", "3")
	if m.Outputs[1].Workspace != "3" {
		t.Errorf("DP-1 workspace = %q, want 3", m.Outputs[1].Workspace)
	}
	// The window stays on workspace 1, which is now hidden.
	arr := m.Arrange()
	if arr.Placements[10].Visible {
		t.Errorf("window on hidden workspace is visible")
	}
	run(t, m, "view", "1")
	arr = m.Arrange()
	if !arr.Placements[10].Visible {
		t.Errorf("window not visible after viewing its workspace again")
	}
}

func TestViewVisibleElsewhereFocusesThatOutput(t *testing.T) {
	m := twoOutputs()
	// Workspace 2 is visible on output 2. Viewing it from output 1 should
	// just focus output 2 (xmonad view semantics).
	run(t, m, "view", "2")
	if m.FocusedOutput != 2 {
		t.Errorf("focused output = %d, want 2", m.FocusedOutput)
	}
	if m.Outputs[1].Workspace != "1" || m.Outputs[2].Workspace != "2" {
		t.Errorf("workspaces moved: %q %q", m.Outputs[1].Workspace, m.Outputs[2].Workspace)
	}
}

func TestPullSwapsVisibleWorkspaces(t *testing.T) {
	m := twoOutputs()
	// Pull workspace 2 (visible on output 2) onto output 1: the outputs
	// swap workspaces (xmonad greedyView semantics).
	run(t, m, "pull", "2")
	if m.Outputs[1].Workspace != "2" || m.Outputs[2].Workspace != "1" {
		t.Errorf("after pull: %q %q, want 2 1", m.Outputs[1].Workspace, m.Outputs[2].Workspace)
	}
	if m.FocusedOutput != 1 {
		t.Errorf("focused output = %d, want 1", m.FocusedOutput)
	}
}

func TestSendMovesWindow(t *testing.T) {
	m := twoOutputs()
	m.WindowAdded(10)
	m.WindowAdded(11)
	run(t, m, "send", "5")
	// 11 was focused; it moves to workspace 5.
	if m.Windows[11].Workspace != "5" {
		t.Errorf("window 11 workspace = %q, want 5", m.Windows[11].Workspace)
	}
	if got := m.Workspaces["1"].Windows; len(got) != 1 || got[0] != 10 {
		t.Errorf("workspace 1 windows = %v, want [10]", got)
	}
	if got := m.Workspaces["5"].Windows; len(got) != 1 || got[0] != 11 {
		t.Errorf("workspace 5 windows = %v, want [11]", got)
	}
	// Focus on workspace 1 falls back to window 10.
	if fw := m.FocusedWindow(); fw == nil || fw.ID != 10 {
		t.Errorf("FocusedWindow = %v, want 10", fw)
	}
}

func TestSendToOutputDirectional(t *testing.T) {
	m := twoOutputs()
	m.WindowAdded(10)
	run(t, m, "send-to-output", "right")
	if m.Windows[10].Workspace != "2" {
		t.Errorf("window workspace = %q, want 2 (visible on the right output)", m.Windows[10].Workspace)
	}
	// Sending left from output 1 goes nowhere (no output to the left).
	m.WindowAdded(11)
	run(t, m, "send-to-output", "left")
	if m.Windows[11].Workspace != "1" {
		t.Errorf("window moved despite no output to the left")
	}
}

func TestFocusOutputDirectional(t *testing.T) {
	m := twoOutputs()
	run(t, m, "focus-output", "right")
	if m.FocusedOutput != 2 {
		t.Errorf("focused output = %d, want 2", m.FocusedOutput)
	}
	run(t, m, "focus-output", "left")
	if m.FocusedOutput != 1 {
		t.Errorf("focused output = %d, want 1", m.FocusedOutput)
	}
	run(t, m, "focus-output", "next")
	if m.FocusedOutput != 2 {
		t.Errorf("focused output = %d, want 2", m.FocusedOutput)
	}
	run(t, m, "focus-output", "DP-1")
	if m.FocusedOutput != 1 {
		t.Errorf("focused output by name = %d, want 1", m.FocusedOutput)
	}
}

func TestOutputRemovedPreservesWindows(t *testing.T) {
	m := twoOutputs()
	m.WindowAdded(10)
	run(t, m, "focus-output", "right")
	m.WindowAdded(20)

	m.OutputRemoved(2)

	if _, ok := m.Windows[20]; !ok {
		t.Fatal("window 20 lost when its output was removed")
	}
	if m.Windows[20].Workspace != "2" {
		t.Errorf("window 20 workspace = %q, want 2 (workspace survives, hidden)", m.Windows[20].Workspace)
	}
	if m.FocusedOutput != 1 {
		t.Errorf("focused output = %d, want 1", m.FocusedOutput)
	}
	// Workspace 2 is hidden but still reachable.
	run(t, m, "view", "2")
	if m.Outputs[1].Workspace != "2" {
		t.Errorf("could not view orphaned workspace")
	}
	if !m.Arrange().Placements[20].Visible {
		t.Errorf("window 20 not visible after viewing its workspace")
	}
}

func TestLockedModeViewSwitchesAllOutputs(t *testing.T) {
	m := twoOutputs()
	run(t, m, "workspace-mode", "locked")
	run(t, m, "view", "3")
	if m.Outputs[1].Workspace != "3@DP-1" {
		t.Errorf("DP-1 workspace = %q, want 3@DP-1", m.Outputs[1].Workspace)
	}
	if m.Outputs[2].Workspace != "3@HDMI-A-1" {
		t.Errorf("HDMI-A-1 workspace = %q, want 3@HDMI-A-1", m.Outputs[2].Workspace)
	}
	// A window opened now lands on the focused output's expansion.
	m.WindowAdded(10)
	if m.Windows[10].Workspace != "3@DP-1" {
		t.Errorf("window workspace = %q", m.Windows[10].Workspace)
	}
	// send 4 moves it to the focused output's desktop 4.
	run(t, m, "send", "4")
	if m.Windows[10].Workspace != "4@DP-1" {
		t.Errorf("window workspace after send = %q, want 4@DP-1", m.Windows[10].Workspace)
	}
}

func TestDialogFloatsByDefault(t *testing.T) {
	m := twoOutputs()
	m.WindowAdded(10)
	m.WindowAdded(11)
	m.WindowParent(11, 10)
	if !m.Windows[11].Floating {
		t.Errorf("window with a parent did not float")
	}
	// The floating window renders above the tiled one.
	arr := m.Arrange()
	if len(arr.Order) != 2 || arr.Order[len(arr.Order)-1] != 11 {
		t.Errorf("render order = %v, want floating window last (top)", arr.Order)
	}
}

func TestFloatingWindowCenteredOnceDimensionsKnown(t *testing.T) {
	m := twoOutputs()
	m.WindowAdded(10)
	run(t, m, "toggle-float")
	w := m.Windows[10]
	if !w.Floating {
		t.Fatal("not floating")
	}
	// Dimensions arrive: 800x600 on a 1920x1080 output -> centered.
	m.WindowDimensions(10, 800, 600)
	want := Rect{X: (1920 - 800) / 2, Y: (1080 - 600) / 2, W: 800, H: 600}
	if w.FloatRect != want {
		t.Errorf("float rect = %v, want %v", w.FloatRect, want)
	}
}

func TestFullscreen(t *testing.T) {
	m := twoOutputs()
	m.WindowAdded(10)
	m.WindowAdded(11)
	run(t, m, "focus", "main") // focus 10
	run(t, m, "toggle-fullscreen")
	if m.Windows[10].FullscreenOn != 1 {
		t.Errorf("fullscreen on = %d, want output 1", m.Windows[10].FullscreenOn)
	}
	arr := m.Arrange()
	if arr.Placements[10].Fullscreen != 1 {
		t.Errorf("placement fullscreen = %d", arr.Placements[10].Fullscreen)
	}
	// The other window still tiles (it gets the whole area to itself).
	if got := arr.Placements[11].Rect; got != (Rect{X: 0, Y: 0, W: 1920, H: 1080}) {
		t.Errorf("remaining tiled window = %v", got)
	}
	// Fullscreen window renders on top.
	if arr.Order[len(arr.Order)-1] != 10 {
		t.Errorf("render order = %v, want fullscreen window last", arr.Order)
	}
	run(t, m, "toggle-fullscreen")
	if m.Windows[10].FullscreenOn != 0 {
		t.Errorf("still fullscreen after toggle")
	}
}

func TestCloseQueuesRequest(t *testing.T) {
	m := twoOutputs()
	m.WindowAdded(10)
	run(t, m, "close")
	if len(m.CloseRequests) != 1 || m.CloseRequests[0] != 10 {
		t.Errorf("close requests = %v, want [10]", m.CloseRequests)
	}
	// The window is still in the model until the compositor confirms.
	if _, ok := m.Windows[10]; !ok {
		t.Errorf("window removed before compositor confirmed close")
	}
}

func TestWindowInteractionFocuses(t *testing.T) {
	m := twoOutputs()
	m.WindowAdded(10)
	run(t, m, "focus-output", "right")
	m.WindowAdded(20)
	// Clicking window 10 (on output 1) focuses it and its output.
	m.WindowInteracted(10)
	if m.FocusedOutput != 1 {
		t.Errorf("focused output = %d, want 1", m.FocusedOutput)
	}
	if fw := m.FocusedWindow(); fw == nil || fw.ID != 10 {
		t.Errorf("focused window = %v, want 10", fw)
	}
}

func TestUnknownCommandIsUserError(t *testing.T) {
	m := NewModel()
	_, err := m.Dispatch([]string{"frobnicate"})
	if err == nil {
		t.Fatal("expected error")
	}
	var ce *CmdError
	if !asCmdError(err, &ce) {
		t.Errorf("error %T is not a *CmdError", err)
	}
	if !strings.Contains(err.Error(), "frobnicate") {
		t.Errorf("error %q does not mention the command", err)
	}
}

func asCmdError(err error, target **CmdError) bool {
	ce, ok := err.(*CmdError)
	if ok {
		*target = ce
	}
	return ok
}

func TestSetMainRatioAdjustment(t *testing.T) {
	m := twoOutputs()
	ws := m.focusedWorkspace()
	run(t, m, "set", "main-ratio", "0.5")
	if ws.Params.MainRatio != 0.5 {
		t.Errorf("ratio = %v", ws.Params.MainRatio)
	}
	run(t, m, "set", "main-ratio", "+0.1")
	if ws.Params.MainRatio != 0.6 {
		t.Errorf("ratio = %v, want 0.6", ws.Params.MainRatio)
	}
	// Clamped at the maximum.
	run(t, m, "set", "main-ratio", "+5")
	if ws.Params.MainRatio != MaxMainRatio {
		t.Errorf("ratio = %v, want clamped to %v", ws.Params.MainRatio, MaxMainRatio)
	}
}

func TestGetStateIsValidJSON(t *testing.T) {
	m := twoOutputs()
	m.WindowAdded(10)
	out := run(t, m, "get", "state")
	if !strings.Contains(out, "\"DP-1\"") {
		t.Errorf("get state output missing output name:\n%s", out)
	}
	if !strings.Contains(out, "\"focused_output\": \"DP-1\"") {
		t.Errorf("get state output missing focused output:\n%s", out)
	}
}

func TestNoOutputsIsNotFatal(t *testing.T) {
	// Windows can appear before any output does (or after all outputs are
	// gone). They must be tracked and become visible when an output shows
	// up.
	m := NewModel()
	m.WindowAdded(10)
	if m.Windows[10].Workspace != "1" {
		t.Errorf("window parked on %q, want workspace 1", m.Windows[10].Workspace)
	}
	arr := m.Arrange()
	if arr.Placements[10].Visible {
		t.Errorf("window visible with no outputs")
	}
	m.OutputAdded(1, "DP-1", Rect{W: 1920, H: 1080})
	arr = m.Arrange()
	if !arr.Placements[10].Visible {
		t.Errorf("window not visible after an output appeared")
	}
}
