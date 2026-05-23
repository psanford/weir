package core

import "testing"

func TestFixedSizeWindowFloats(t *testing.T) {
	m := twoOutputs()

	// A window with min == max in both dimensions (a pinentry prompt).
	m.WindowAdded(10)
	m.WindowDimensionsHint(10, 400, 200, 400, 200)
	if !m.Windows[10].Floating {
		t.Errorf("fixed-size window did not float")
	}

	// Fixed in one dimension only also floats.
	m.WindowAdded(11)
	m.WindowDimensionsHint(11, 300, 100, 300, 0)
	if !m.Windows[11].Floating {
		t.Errorf("fixed-width window did not float")
	}

	// A window with only a minimum (a normal resizable app) tiles.
	m.WindowAdded(12)
	m.WindowDimensionsHint(12, 200, 100, 0, 0)
	if m.Windows[12].Floating {
		t.Errorf("resizable window with a minimum size floated")
	}

	// No hints at all tiles.
	m.WindowAdded(13)
	m.WindowDimensionsHint(13, 0, 0, 0, 0)
	if m.Windows[13].Floating {
		t.Errorf("window with no hints floated")
	}

	// A no-float rule overrides the fixed-size heuristic regardless of
	// event order.
	run(t, m, "rule", "add", "-app-id", "stubborn", "no-float")
	m.WindowAdded(14)
	m.WindowAppID(14, "stubborn")
	m.WindowDimensionsHint(14, 400, 200, 400, 200)
	if m.Windows[14].Floating {
		t.Errorf("no-float rule did not override the fixed-size float")
	}

	// A fixed-size hint that arrives after the window has already been
	// displayed still floats it: clients often only learn their natural
	// size after their first layout pass.
	m.WindowAdded(15)
	m.WindowDimensions(15, 960, 540)
	m.WindowDimensionsHint(15, 500, 500, 500, 500)
	if !m.Windows[15].Floating {
		t.Errorf("late fixed-size hint did not float the window")
	}
}
