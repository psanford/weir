package core

import "testing"

// TestMonocleFocusedWindowOnTop checks that the focused window renders
// above its siblings in monocle mode, where every window occupies the same
// rectangle.
func TestMonocleFocusedWindowOnTop(t *testing.T) {
	m := twoOutputs()
	for id := WindowID(10); id <= 12; id++ {
		m.WindowAdded(id)
	}
	run(t, m, "set-layout", "monocle")

	top := func() WindowID {
		order := m.Arrange().Order
		if len(order) == 0 {
			t.Fatal("empty order")
		}
		return order[len(order)-1]
	}

	// Window 12 is focused (newest) and must be on top.
	if got := top(); got != 12 {
		t.Fatalf("top window = %d, want 12", got)
	}
	// focus next wraps to window 10: it must come to the top.
	run(t, m, "focus", "next")
	if got := top(); got != 10 {
		t.Errorf("after focus next, top window = %d, want 10", got)
	}
	run(t, m, "focus", "next")
	if got := top(); got != 11 {
		t.Errorf("after focus next x2, top window = %d, want 11", got)
	}
	// Every window still appears exactly once in the order.
	seen := map[WindowID]int{}
	for _, id := range m.Arrange().Order {
		seen[id]++
	}
	if len(seen) != 3 || seen[10] != 1 || seen[11] != 1 || seen[12] != 1 {
		t.Errorf("order is not a permutation: %v", m.Arrange().Order)
	}
}

// TestFocusedFloatingWindowOnTop checks that focusing one of two
// overlapping floating windows raises it.
func TestFocusedFloatingWindowOnTop(t *testing.T) {
	m := twoOutputs()
	m.WindowAdded(10)
	run(t, m, "toggle-float")
	m.WindowAdded(11)
	run(t, m, "toggle-float")

	order := m.Arrange().Order
	if order[len(order)-1] != 11 {
		t.Fatalf("top = %v, want 11 (focused)", order)
	}
	// Click window 10: it comes to the front.
	m.WindowInteracted(10)
	order = m.Arrange().Order
	if order[len(order)-1] != 10 {
		t.Errorf("after focusing 10, top = %v, want 10", order)
	}
}

// TestFloatingStillAboveTiled checks that raising the focused tiled window
// never lifts it above floating windows.
func TestFloatingStillAboveTiled(t *testing.T) {
	m := twoOutputs()
	m.WindowAdded(10)
	m.WindowAdded(11)
	run(t, m, "toggle-float") // 11 floats
	run(t, m, "focus", "next")
	if fw := m.FocusedWindow(); fw == nil || fw.ID != 10 {
		t.Fatalf("setup: focused = %v", fw)
	}
	order := m.Arrange().Order
	if order[len(order)-1] != 11 {
		t.Errorf("floating window not on top: %v", order)
	}
}
