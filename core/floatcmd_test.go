package core

import "testing"

func TestMoveFloatsAndShifts(t *testing.T) {
	m := twoOutputs()
	m.WindowAdded(10)
	m.WindowAdded(11)
	// Window 11 (focused) is the stack window at 1152,0 768x1080.
	run(t, m, "move", "left", "100")
	w := m.Windows[11]
	if !w.Floating {
		t.Fatal("move did not float the tiled window")
	}
	if w.FloatRect != (Rect{X: 1052, Y: 0, W: 768, H: 1080}) {
		t.Errorf("after move left 100: %v", w.FloatRect)
	}
	run(t, m, "move", "down", "50")
	if w.FloatRect.Y != 50 {
		t.Errorf("after move down 50: %v", w.FloatRect)
	}
	if _, err := m.Dispatch([]string{"move", "sideways", "10"}); err == nil {
		t.Error("invalid direction accepted")
	}
}

func TestSnapToEdges(t *testing.T) {
	m := twoOutputs()
	m.WindowAdded(10)
	run(t, m, "toggle-float")
	w := m.Windows[10]
	w.FloatRect = Rect{X: 500, Y: 300, W: 400, H: 200}
	run(t, m, "snap", "left")
	if w.FloatRect.X != 0 {
		t.Errorf("snap left: %v", w.FloatRect)
	}
	run(t, m, "snap", "down")
	if w.FloatRect.Y != 1080-200 {
		t.Errorf("snap down: %v", w.FloatRect)
	}
	run(t, m, "snap", "right")
	if w.FloatRect.X != 1920-400 {
		t.Errorf("snap right: %v", w.FloatRect)
	}
}

func TestResizeKeepsCenterAndRespectsHints(t *testing.T) {
	m := twoOutputs()
	m.WindowAdded(10)
	run(t, m, "toggle-float")
	w := m.Windows[10]
	w.FloatRect = Rect{X: 500, Y: 300, W: 400, H: 200}
	run(t, m, "resize", "horizontal", "100")
	if w.FloatRect != (Rect{X: 450, Y: 300, W: 500, H: 200}) {
		t.Errorf("resize horizontal +100: %v", w.FloatRect)
	}
	// Shrinking below the client minimum clamps.
	m.WindowDimensionsHint(10, 300, 100, 0, 0)
	run(t, m, "resize", "horizontal", "-1000")
	if w.FloatRect.W != 300 {
		t.Errorf("resize below min width: %v", w.FloatRect)
	}
}

func TestViewNextPrev(t *testing.T) {
	m := twoOutputs()
	// DP-1 shows 1, HDMI shows 2. view next from 1 should go to 2... but 2
	// is visible on the other output, so view focuses that output instead
	// (xmonad view semantics). Put a window on 5 to make the cycle
	// interesting and verify wrap-around.
	m.WindowAdded(10)
	run(t, m, "send", "5")
	run(t, m, "view", "next") // 1 -> 2: visible on output 2, so focus moves there
	if m.FocusedOutput != 2 {
		t.Fatalf("view next onto a visible workspace should focus its output")
	}
	run(t, m, "view", "next") // 2 -> 3
	if m.Outputs[2].Workspace != "3" {
		t.Fatalf("view next = %q, want 3", m.Outputs[2].Workspace)
	}
	run(t, m, "view", "prev") // 3 -> 2
	if m.Outputs[2].Workspace != "2" {
		t.Fatalf("view prev = %q, want 2", m.Outputs[2].Workspace)
	}
	// Wrap backwards from 1.
	run(t, m, "focus-output", "DP-1")
	run(t, m, "view", "prev") // 1 -> 9 (last default; workspace 5 is in the defaults already)
	if m.Outputs[1].Workspace != "9" {
		t.Fatalf("view prev from 1 = %q, want 9 (wrap)", m.Outputs[1].Workspace)
	}
}

func TestFocusFollowsCursorPolicy(t *testing.T) {
	m := twoOutputs()
	m.WindowAdded(10)
	m.WindowAdded(11)
	run(t, m, "focus", "main")
	// Disabled by default: pointer enter does not move focus.
	m.PointerEntered(11)
	if fw := m.FocusedWindow(); fw == nil || fw.ID != 10 {
		t.Fatalf("focus moved on hover while focus-follows-cursor is off")
	}
	run(t, m, "set", "focus-follows-cursor", "on")
	m.PointerEntered(11)
	if fw := m.FocusedWindow(); fw == nil || fw.ID != 11 {
		t.Fatalf("focus did not follow the cursor when enabled")
	}
}
