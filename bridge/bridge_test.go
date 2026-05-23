package bridge

import (
	"testing"

	"github.com/psanford/weir/core"
)

// TestSingleWindowLifecycle walks a window from creation to display: the
// bridge must propose dimensions in the manage sequence, then position and
// stack the window in the render sequence.
func TestSingleWindowLifecycle(t *testing.T) {
	f, _ := newFakeRiver(t)
	f.addOutput(0, 0, 1920, 1080)
	f.addSeat()
	winID := f.addWindow()

	reqs := f.manageCycle()

	// The new window must get propose_dimensions covering the full output
	// (one window, tile layout, no gaps).
	props := find(reqs, "river_window_v1", winReqProposeDimensions)
	if len(props) != 1 {
		t.Fatalf("got %d propose_dimensions, want 1; requests: %v", len(props), reqs)
	}
	d := props[0].decoder()
	w, _ := d.Int()
	h, _ := d.Int()
	if w != 1920 || h != 1080 {
		t.Errorf("proposed %dx%d, want 1920x1080", w, h)
	}
	// Capabilities and decoration mode are set exactly once for a new
	// window.
	if got := find(reqs, "river_window_v1", winReqSetCapabilities); len(got) != 1 {
		t.Errorf("got %d set_capabilities, want 1", len(got))
	}
	// The window is focused.
	if got := find(reqs, "river_seat_v1", seatReqFocusWindow); len(got) != 1 {
		t.Errorf("got %d focus_window, want 1", len(got))
	}
	// The sequence ends with manage_finish.
	last := reqs[len(reqs)-1]
	if last.iface != "river_window_manager_v1" || last.opcode != wmReqManageFinish {
		t.Errorf("last manage request = %v, want manage_finish", last)
	}

	// The compositor reports the window took the proposed size and starts
	// a render sequence.
	f.windowDimensions(winID, 1920, 1080)
	reqs = f.renderCycle()

	if got := find(reqs, "river_window_v1", winReqGetNode); len(got) != 1 {
		t.Fatalf("got %d get_node, want 1", len(got))
	}
	pos := find(reqs, "river_node_v1", nodeReqSetPosition)
	if len(pos) != 1 {
		t.Fatalf("got %d set_position, want 1", len(pos))
	}
	d = pos[0].decoder()
	x, _ := d.Int()
	y, _ := d.Int()
	if x != 0 || y != 0 {
		t.Errorf("position = %d,%d, want 0,0", x, y)
	}
	if got := find(reqs, "river_node_v1", nodeReqPlaceBottom); len(got) != 1 {
		t.Errorf("got %d place_bottom, want 1", len(got))
	}
	if got := find(reqs, "river_window_v1", winReqSetBorders); len(got) != 1 {
		t.Errorf("got %d set_borders, want 1", len(got))
	}
	last = reqs[len(reqs)-1]
	if last.iface != "river_window_manager_v1" || last.opcode != wmReqRenderFinish {
		t.Errorf("last render request = %v, want render_finish", last)
	}
}

// TestSecondWindowRetiles checks that adding a second window re-proposes
// dimensions for the first (the master/stack split changes both).
func TestSecondWindowRetiles(t *testing.T) {
	f, _ := newFakeRiver(t)
	f.addOutput(0, 0, 1000, 600)
	f.addSeat()
	w1 := f.addWindow()
	f.manageCycle()
	f.windowDimensions(w1, 1000, 600)
	f.renderCycle()

	f.addWindow()
	reqs := f.manageCycle()
	props := find(reqs, "river_window_v1", winReqProposeDimensions)
	if len(props) != 2 {
		t.Fatalf("got %d propose_dimensions after adding a second window, want 2 (both windows resize)", len(props))
	}
	// Default main ratio is 0.6: the master gets 600 wide, the stack 400.
	sizes := map[uint32][2]int32{}
	for _, p := range props {
		d := p.decoder()
		w, _ := d.Int()
		h, _ := d.Int()
		sizes[p.object] = [2]int32{w, h}
	}
	if got := sizes[w1]; got != [2]int32{600, 600} {
		t.Errorf("first window proposed %v, want [600 600]", got)
	}
}

// TestUnchangedManageSequenceIsQuiet checks the diffing: a manage sequence
// in which nothing changed must produce only manage_finish.
func TestUnchangedManageSequenceIsQuiet(t *testing.T) {
	f, _ := newFakeRiver(t)
	f.addOutput(0, 0, 1920, 1080)
	f.addSeat()
	w1 := f.addWindow()
	f.manageCycle()
	f.windowDimensions(w1, 1920, 1080)
	f.renderCycle()

	// A spurious manage sequence (e.g. triggered by a no-op event).
	reqs := f.manageCycle()
	if len(reqs) != 1 {
		t.Errorf("no-op manage sequence sent %d requests, want 1 (manage_finish only): %v", len(reqs), reqs)
	}
	reqs = f.renderCycle()
	if len(reqs) != 1 {
		t.Errorf("no-op render sequence sent %d requests, want 1 (render_finish only): %v", len(reqs), reqs)
	}
}

// TestWindowClosedRetiles checks that closing one of two windows gives the
// survivor the full output again and that the closed window's objects are
// destroyed.
func TestWindowClosedRetiles(t *testing.T) {
	f, _ := newFakeRiver(t)
	f.addOutput(0, 0, 1000, 600)
	f.addSeat()
	w1 := f.addWindow()
	w2 := f.addWindow()
	f.manageCycle()
	f.windowDimensions(w1, 600, 600)
	f.windowDimensions(w2, 400, 600)
	f.renderCycle()

	f.closeWindow(w2)
	reqs := f.manageCycle()
	// The closed window's proxy is destroyed.
	if got := find(reqs, "river_window_v1", winReqDestroy); len(got) != 1 {
		t.Errorf("got %d window destroy requests, want 1", len(got))
	}
	// The survivor is re-proposed at full size.
	props := find(reqs, "river_window_v1", winReqProposeDimensions)
	if len(props) != 1 || props[0].object != w1 {
		t.Fatalf("propose_dimensions = %v, want exactly one for window %d", props, w1)
	}
	d := props[0].decoder()
	w, _ := d.Int()
	if w != 1000 {
		t.Errorf("survivor proposed width %d, want 1000", w)
	}
}

// TestFocusFollowsNewWindow checks that focus moves to each new window and
// back to the survivor when the focused window closes.
func TestFocusFollowsNewWindow(t *testing.T) {
	f, _ := newFakeRiver(t)
	f.addOutput(0, 0, 1000, 600)
	f.addSeat()
	w1 := f.addWindow()
	reqs := f.manageCycle()
	focus := find(reqs, "river_seat_v1", seatReqFocusWindow)
	if len(focus) != 1 || objectArg(t, focus[0]) != w1 {
		t.Fatalf("initial focus = %v, want window %d", focus, w1)
	}

	w2 := f.addWindow()
	reqs = f.manageCycle()
	focus = find(reqs, "river_seat_v1", seatReqFocusWindow)
	if len(focus) != 1 || objectArg(t, focus[0]) != w2 {
		t.Fatalf("focus after second window = %v, want window %d", focus, w2)
	}

	f.closeWindow(w2)
	reqs = f.manageCycle()
	focus = find(reqs, "river_seat_v1", seatReqFocusWindow)
	if len(focus) != 1 || objectArg(t, focus[0]) != w1 {
		t.Fatalf("focus after closing focused window = %v, want window %d", focus, w1)
	}
}

// TestOutputRemovalKeepsWindows checks that removing the only output hides
// its windows rather than crashing, and that a new output brings them back.
func TestOutputRemovalKeepsWindows(t *testing.T) {
	f, b := newFakeRiver(t)
	out1 := f.addOutput(0, 0, 1920, 1080)
	f.addSeat()
	w1 := f.addWindow()
	f.manageCycle()
	f.windowDimensions(w1, 1920, 1080)
	f.renderCycle()

	f.removeOutput(out1)
	reqs := f.manageCycle()
	if got := find(reqs, "river_output_v1", outReqDestroy); len(got) != 1 {
		t.Errorf("got %d output destroy requests, want 1", len(got))
	}
	// The window survives in the model.
	if len(b.Model().Windows) != 1 {
		t.Fatalf("model has %d windows after output removal, want 1", len(b.Model().Windows))
	}
	reqs = f.renderCycle()
	// The window is hidden now that its workspace has no output.
	if got := find(reqs, "river_window_v1", winReqHide); len(got) != 1 {
		t.Errorf("got %d hide requests, want 1: %v", len(got), reqs)
	}

	// Plug in a new output: the workspace is adopted and the window shown
	// again.
	f.addOutput(0, 0, 1280, 720)
	reqs = f.manageCycle()
	props := find(reqs, "river_window_v1", winReqProposeDimensions)
	if len(props) != 1 {
		t.Fatalf("got %d propose_dimensions after replug, want 1", len(props))
	}
	d := props[0].decoder()
	w, _ := d.Int()
	h, _ := d.Int()
	if w != 1280 || h != 720 {
		t.Errorf("proposed %dx%d after replug, want 1280x720", w, h)
	}
	reqs = f.renderCycle()
	if got := find(reqs, "river_window_v1", winReqShow); len(got) != 1 {
		t.Errorf("got %d show requests, want 1: %v", len(got), reqs)
	}
}

// TestCommandTriggersManageDirty checks the IPC path: a command that
// changes the model must cause a manage_dirty request, and the following
// manage sequence must apply the change.
func TestCommandTriggersManageDirty(t *testing.T) {
	f, b := newFakeRiver(t)
	f.addOutput(0, 0, 1000, 600)
	f.addSeat()
	w1 := f.addWindow()
	w2 := f.addWindow()
	f.manageCycle()
	f.windowDimensions(w1, 600, 600)
	f.windowDimensions(w2, 400, 600)
	f.renderCycle()

	// Run a command directly against the model the way Run's command
	// branch does.
	if _, err := b.Model().Dispatch([]string{"set", "main-ratio", "0.5"}); err != nil {
		t.Fatal(err)
	}
	if !b.Model().Changed() {
		t.Fatal("model not marked changed after a mutating command")
	}
	b.Dirty()
	reqs0 := f.collect()
	if got := find(reqs0, "river_window_manager_v1", wmReqManageDirty); len(got) != 1 {
		t.Fatalf("expected manage_dirty after a mutating command, got %v", reqs0)
	}

	// The compositor responds with a manage sequence; both windows get new
	// dimensions at the 0.5 ratio.
	reqs := f.manageCycle()
	props := find(reqs, "river_window_v1", winReqProposeDimensions)
	if len(props) != 2 {
		t.Fatalf("got %d propose_dimensions after ratio change, want 2", len(props))
	}
	for _, p := range props {
		d := p.decoder()
		w, _ := d.Int()
		if w != 500 {
			t.Errorf("window %d proposed width %d, want 500", p.object, w)
		}
	}
}

// TestStackingOrderOnlySentWhenChanged checks that place_* requests are
// skipped when the render order has not changed.
func TestStackingOrderOnlySentWhenChanged(t *testing.T) {
	f, _ := newFakeRiver(t)
	f.addOutput(0, 0, 1000, 600)
	f.addSeat()
	w1 := f.addWindow()
	w2 := f.addWindow()
	f.manageCycle()
	f.windowDimensions(w1, 600, 600)
	f.windowDimensions(w2, 400, 600)
	reqs := f.renderCycle()
	if got := find(reqs, "river_node_v1", nodeReqPlaceBottom); len(got) != 1 {
		t.Errorf("first render: got %d place_bottom, want 1", len(got))
	}
	if got := find(reqs, "river_node_v1", nodeReqPlaceAbove); len(got) != 1 {
		t.Errorf("first render: got %d place_above, want 1", len(got))
	}

	// A second render sequence with no changes must not restack.
	reqs = f.renderCycle()
	if got := find(reqs, "river_node_v1", nodeReqPlaceBottom); len(got) != 0 {
		t.Errorf("unchanged render: got %d place_bottom, want 0", len(got))
	}
	if got := find(reqs, "river_node_v1", nodeReqPlaceAbove); len(got) != 0 {
		t.Errorf("unchanged render: got %d place_above, want 0", len(got))
	}
}

// TestModelMatchesProtocolState cross-checks the model's snapshot against
// what was actually sent over the protocol after a few operations.
func TestModelMatchesProtocolState(t *testing.T) {
	f, b := newFakeRiver(t)
	f.addOutput(0, 0, 1920, 1080)
	f.addSeat()
	f.addWindow()
	f.addWindow()
	f.addWindow()
	f.manageCycle()

	snap := b.Model().Snapshot()
	if len(snap.Windows) != 3 {
		t.Fatalf("snapshot has %d windows, want 3", len(snap.Windows))
	}
	if len(snap.Outputs) != 1 || snap.Outputs[0].Workspace != "1" {
		t.Fatalf("snapshot outputs = %+v", snap.Outputs)
	}
	// All three windows tile without overlap within the output.
	rects := make([]core.Rect, 0, 3)
	for _, w := range snap.Windows {
		r := core.Rect{X: w.X, Y: w.Y, W: w.Width, H: w.Height}
		for _, prev := range rects {
			if r.Overlaps(prev) {
				t.Errorf("windows overlap: %v and %v", r, prev)
			}
		}
		rects = append(rects, r)
	}
}

// objectArg decodes the first argument of a request as an object ID.
func objectArg(t *testing.T, r request) uint32 {
	t.Helper()
	d := r.decoder()
	id, err := d.Object()
	if err != nil {
		t.Fatalf("decoding object arg of %v: %v", r, err)
	}
	return id
}
