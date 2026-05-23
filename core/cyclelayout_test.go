package core

import "testing"

func TestCycleLayout(t *testing.T) {
	m := twoOutputs()
	ws := m.focusedWorkspace()
	// Default state: tile, main on the left.
	if got := currentLayoutSpec(ws); got != "left" {
		t.Fatalf("initial layout spec = %q", got)
	}

	// left is in the list, so the first invocation advances to the next
	// entry after it (wrapping to monocle).
	run(t, m, "cycle-layout", "monocle,left,top")
	if ws.Layout != LayoutTile || ws.Params.MainLocation != MainTop {
		t.Fatalf("after cycle 1: layout=%v loc=%v, want tile/top", ws.Layout, ws.Params.MainLocation)
	}
	run(t, m, "cycle-layout", "monocle,left,top")
	if ws.Layout != LayoutMonocle {
		t.Fatalf("after cycle 2: layout=%v, want monocle", ws.Layout)
	}
	// Monocle preserved the main location for the trip back to tile.
	run(t, m, "cycle-layout", "monocle,left,top")
	if ws.Layout != LayoutTile || ws.Params.MainLocation != MainLeft {
		t.Fatalf("after cycle 3: layout=%v loc=%v, want tile/left", ws.Layout, ws.Params.MainLocation)
	}

	// A current state outside the list jumps to the first entry.
	run(t, m, "set", "main-location", "right")
	run(t, m, "cycle-layout", "monocle,left,top")
	if ws.Layout != LayoutMonocle {
		t.Fatalf("from outside the list: layout=%v, want monocle (first entry)", ws.Layout)
	}

	// The cycle is per workspace.
	run(t, m, "focus-output", "right")
	if got := currentLayoutSpec(m.focusedWorkspace()); got != "left" {
		t.Errorf("other workspace was affected: %q", got)
	}

	// Errors.
	for _, bad := range [][]string{
		{"cycle-layout"},
		{"cycle-layout", "monocle"},
		{"cycle-layout", "monocle,sideways"},
		{"cycle-layout", "monocle,left", "extra"},
	} {
		if _, err := m.Dispatch(bad); err == nil {
			t.Errorf("Dispatch(%v) succeeded, want error", bad)
		}
	}
}
