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

// TestSingleOutputDPMSRestoresWorkspace reproduces the laptop DPMS case: a
// single output showing a non-default workspace blanks (output destroyed)
// and comes back (re-added under a synthetic name, then renamed to its real
// name). The workspace the user was on must be restored even though the
// interim auto-assigned workspace (workspace 1) has windows on it.
func TestSingleOutputDPMSRestoresWorkspace(t *testing.T) {
	m := NewModel()
	m.Borders.Width = 0
	m.OutputAdded(1, "output-1", Rect{W: 2560, H: 1600})
	m.OutputRenamed(1, "eDP-1")

	// Windows on workspace 1 and workspace 3; the user is working on 3.
	m.WindowAdded(10)
	run(t, m, "view", "3")
	m.WindowAdded(11)
	if m.Outputs[1].Workspace != "3" {
		t.Fatalf("setup: workspace = %q, want 3", m.Outputs[1].Workspace)
	}

	// Screen blanks: the output is destroyed.
	m.OutputRemoved(1)

	// Screen unblanks: new output, synthetic name first, then the rename.
	m.OutputAdded(2, "output-2", Rect{W: 2560, H: 1600})
	m.OutputRenamed(2, "eDP-1")

	if got := m.Outputs[2].Workspace; got != "3" {
		t.Errorf("after DPMS cycle the output shows %q, want 3 (the workspace the user was on)", got)
	}
	if !m.Arrange().Placements[11].Visible {
		t.Errorf("the window the user was working on is not visible after the screen came back")
	}
}

// TestRenameDoesNotOverrideUserChoice checks the guard the restoration must
// keep: if the user explicitly views a workspace on the new output before
// the rename event arrives, the rename must not yank it away.
func TestRenameDoesNotOverrideUserChoice(t *testing.T) {
	m := NewModel()
	m.Borders.Width = 0
	m.OutputAdded(1, "output-1", Rect{W: 2560, H: 1600})
	m.OutputRenamed(1, "eDP-1")
	run(t, m, "view", "3")
	m.OutputRemoved(1)

	m.OutputAdded(2, "output-2", Rect{W: 2560, H: 1600})
	// The user views workspace 5 before the rename arrives.
	run(t, m, "view", "5")
	m.OutputRenamed(2, "eDP-1")

	if got := m.Outputs[2].Workspace; got != "5" {
		t.Errorf("rename overrode the user's explicit choice: showing %q, want 5", got)
	}
}
