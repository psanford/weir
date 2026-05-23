package bridge

import (
	"testing"

	"github.com/psanford/weir/wire"
)

// Layer shell opcodes, from declaration order in river-layer-shell-v1.xml.
const (
	lsReqDestroy   = 0
	lsReqGetOutput = 1
	lsReqGetSeat   = 2

	lsOutputReqDestroy    = 0
	lsOutputReqSetDefault = 1

	lsOutputEvNonExclusiveArea = 0

	lsSeatReqDestroy = 0

	lsSeatEvFocusExclusive    = 0
	lsSeatEvFocusNonExclusive = 1
	lsSeatEvFocusNone         = 2
)

// TestLayerShellExclusiveZoneShrinksTiling checks that a bar reserving an
// exclusive zone causes windows to tile in the remaining area, and that
// removing the reservation restores the full output.
func TestLayerShellExclusiveZoneShrinksTiling(t *testing.T) {
	f, _ := newFakeRiver(t)
	out := f.addOutput(0, 0, 1920, 1080)
	f.addSeat()
	w1 := f.addWindow()
	f.manageCycle()
	f.windowDimensions(w1, 1920, 1080)
	f.renderCycle()

	lsOut := f.layerShellOutputs[out]
	if lsOut == 0 {
		t.Fatal("bridge did not create a layer shell output object")
	}

	// A 30px bar at the top reserves its zone: the usable area becomes
	// (0,30) 1920x1050.
	e := &wire.Encoder{}
	e.PutInt(0)
	e.PutInt(30)
	e.PutInt(1920)
	e.PutInt(1050)
	f.server.Send(lsOut, lsOutputEvNonExclusiveArea, e)
	reqs := f.manageCycle()
	props := find(reqs, "river_window_v1", winReqProposeDimensions)
	if len(props) != 1 {
		t.Fatalf("got %d propose_dimensions after exclusive zone change, want 1", len(props))
	}
	d := props[0].decoder()
	w, _ := d.Int()
	h, _ := d.Int()
	if w != 1920 || h != 1050 {
		t.Errorf("window proposed %dx%d, want 1920x1050 (output minus the bar)", w, h)
	}
	f.windowDimensions(w1, 1920, 1050)
	reqs = f.renderCycle()
	pos := find(reqs, "river_node_v1", nodeReqSetPosition)
	if len(pos) != 1 {
		t.Fatalf("got %d set_position, want 1", len(pos))
	}
	d = pos[0].decoder()
	x, _ := d.Int()
	y, _ := d.Int()
	if x != 0 || y != 30 {
		t.Errorf("window positioned at %d,%d, want 0,30 (below the bar)", x, y)
	}

	// The bar goes away: the full output becomes usable again.
	e = &wire.Encoder{}
	e.PutInt(0)
	e.PutInt(0)
	e.PutInt(1920)
	e.PutInt(1080)
	f.server.Send(lsOut, lsOutputEvNonExclusiveArea, e)
	reqs = f.manageCycle()
	props = find(reqs, "river_window_v1", winReqProposeDimensions)
	if len(props) != 1 {
		t.Fatalf("got %d propose_dimensions after the bar left, want 1", len(props))
	}
	d = props[0].decoder()
	w, _ = d.Int()
	h, _ = d.Int()
	if w != 1920 || h != 1080 {
		t.Errorf("window proposed %dx%d after the bar left, want 1920x1080", w, h)
	}
}

// TestLayerShellFocusHandoff checks that focus is re-asserted on the
// focused window after a layer surface (e.g. a launcher) releases keyboard
// focus.
func TestLayerShellFocusHandoff(t *testing.T) {
	f, _ := newFakeRiver(t)
	f.addOutput(0, 0, 1000, 600)
	f.addSeat()
	w1 := f.addWindow()
	f.manageCycle()
	f.windowDimensions(w1, 1000, 600)
	f.renderCycle()
	if f.layerShellSeatID == 0 {
		t.Fatal("bridge did not create a layer shell seat object")
	}

	// A launcher opens and takes exclusive focus.
	f.server.Send(f.layerShellSeatID, lsSeatEvFocusExclusive, &wire.Encoder{})
	reqs := f.manageCycle()
	// weir must not fight over focus while the layer surface holds it.
	if got := find(reqs, "river_seat_v1", seatReqFocusWindow); len(got) != 0 {
		t.Errorf("focus_window sent while a layer surface has exclusive focus")
	}

	// The launcher closes.
	f.server.Send(f.layerShellSeatID, lsSeatEvFocusNone, &wire.Encoder{})
	reqs = f.manageCycle()
	focus := find(reqs, "river_seat_v1", seatReqFocusWindow)
	if len(focus) != 1 || objectArg(t, focus[0]) != w1 {
		t.Fatalf("focus not re-asserted after the layer surface closed: %v", reqs)
	}
}

// TestLayerShellDefaultOutputFollowsFocus checks that the focused output is
// marked as the default for layer surfaces.
func TestLayerShellDefaultOutputFollowsFocus(t *testing.T) {
	f, b := newFakeRiver(t)
	out1 := f.addOutput(0, 0, 1000, 600)
	out2 := f.addOutput(1000, 0, 1000, 600)
	f.addSeat()
	reqs := f.manageCycle()
	defaults := find(reqs, "river_layer_shell_output_v1", lsOutputReqSetDefault)
	if len(defaults) != 1 || defaults[0].object != f.layerShellOutputs[out1] {
		t.Fatalf("expected set_default on output 1's layer shell object, got %v", defaults)
	}
	f.renderCycle()

	if _, err := b.runCommand([]string{"focus-output", "next"}); err != nil {
		t.Fatal(err)
	}
	b.Dirty()
	f.collect()
	reqs = f.manageCycle()
	defaults = find(reqs, "river_layer_shell_output_v1", lsOutputReqSetDefault)
	if len(defaults) != 1 || defaults[0].object != f.layerShellOutputs[out2] {
		t.Fatalf("expected set_default to follow focus to output 2, got %v", defaults)
	}
}
