package core

import "testing"

// TestOutputReplugRestoresWorkspace checks that unplugging a monitor and
// plugging it back in restores the workspace it was showing, including when
// the real output name only arrives via a rename after the output is added.
func TestOutputReplugRestoresWorkspace(t *testing.T) {
	m := twoOutputs()
	// Put a window on workspace 7 and show it on HDMI-A-1.
	run(t, m, "focus-output", "right")
	m.WindowAdded(10)
	run(t, m, "send", "7")
	run(t, m, "view", "7")
	if m.Outputs[2].Workspace != "7" {
		t.Fatalf("setup: HDMI-A-1 shows %q", m.Outputs[2].Workspace)
	}

	m.OutputRemoved(2)
	if got := m.workspaceVisibleOn("7"); got != 0 {
		t.Fatalf("workspace 7 still visible on output %d after removal", got)
	}

	// The monitor comes back: first with a synthetic name, then renamed to
	// its real name (mirroring how the bridge learns names).
	m.OutputAdded(3, "output-3", Rect{X: 1920, Y: 0, W: 2560, H: 1440})
	m.OutputRenamed(3, "HDMI-A-1")
	if m.Outputs[3].Workspace != "7" {
		t.Errorf("re-plugged output shows %q, want 7 (restored)", m.Outputs[3].Workspace)
	}
	if !m.Arrange().Placements[10].Visible {
		t.Errorf("window on the restored workspace is not visible")
	}
}

// TestOutputReplugDoesNotStealVisibleWorkspace checks that restoration does
// not yank a workspace that is now visible on another output.
func TestOutputReplugDoesNotStealVisibleWorkspace(t *testing.T) {
	m := twoOutputs()
	run(t, m, "focus-output", "right")
	run(t, m, "view", "7")
	m.OutputRemoved(2)
	// While the monitor is unplugged, the user views workspace 7 on DP-1.
	run(t, m, "view", "7")
	if m.Outputs[1].Workspace != "7" {
		t.Fatal("setup failed")
	}
	m.OutputAdded(3, "output-3", Rect{X: 1920, Y: 0, W: 2560, H: 1440})
	m.OutputRenamed(3, "HDMI-A-1")
	if m.Outputs[3].Workspace == "7" {
		t.Errorf("re-plugged output stole workspace 7 from DP-1")
	}
	if m.Outputs[1].Workspace != "7" {
		t.Errorf("DP-1 lost workspace 7: now showing %q", m.Outputs[1].Workspace)
	}
}

// TestOutputReplugDirectName checks restoration when the real name is known
// at add time (as in tests and some compositor configurations).
func TestOutputReplugDirectName(t *testing.T) {
	m := twoOutputs()
	run(t, m, "focus-output", "right")
	run(t, m, "view", "9")
	m.OutputRemoved(2)
	m.OutputAdded(3, "HDMI-A-1", Rect{X: 1920, Y: 0, W: 2560, H: 1440})
	if m.Outputs[3].Workspace != "9" {
		t.Errorf("re-plugged output shows %q, want 9", m.Outputs[3].Workspace)
	}
}
