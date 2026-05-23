package bridge

import (
	"strings"
	"testing"

	"github.com/psanford/weir/core"
	"github.com/psanford/weir/wire"
)

const (
	xkbConfigReqCreateKeymap = 2
	xkbConfigEvXkbKeyboard   = 1

	xkbKeymapEvSuccess = 0
	xkbKeymapEvFailure = 1

	xkbKeyboardReqSetKeymap = 1
	xkbKeyboardEvInputDev   = 1

	inputManagerEvInputDevice = 1
	inputDeviceEvType         = 1
	inputDeviceEvName         = 2
)

// addKeyboard announces an input device of type keyboard with the given
// name plus its corresponding xkb keyboard, returning the xkb keyboard's
// object ID.
func (f *fakeRiver) addKeyboard(name string) (devID, xkbKbID uint32) {
	devID = f.allocServerID("river_input_device_v1")
	e := &wire.Encoder{}
	e.PutUint(devID)
	f.server.Send(f.inputManagerID, inputManagerEvInputDevice, e)
	e = &wire.Encoder{}
	e.PutUint(0) // keyboard
	f.server.Send(devID, inputDeviceEvType, e)
	e = &wire.Encoder{}
	e.PutString(name)
	f.server.Send(devID, inputDeviceEvName, e)

	xkbKbID = f.allocServerID("river_xkb_keyboard_v1")
	e = &wire.Encoder{}
	e.PutUint(xkbKbID)
	f.server.Send(f.xkbConfigID, xkbConfigEvXkbKeyboard, e)
	e = &wire.Encoder{}
	e.PutObject(devID)
	f.server.Send(xkbKbID, xkbKeyboardEvInputDev, e)
	return devID, xkbKbID
}

// TestKeyboardLayoutPerDevice checks the full keymap flow: two keyboards,
// one layout for everything and an override for one device by glob. Each
// distinct layout is compiled once, and set_keymap is only sent after the
// compositor confirms the keymap compiled.
func TestKeyboardLayoutPerDevice(t *testing.T) {
	f, b := newFakeRiver(t)
	var compiled []string
	b.CompileKeymap = func(k core.KeyboardLayout) (string, error) {
		compiled = append(compiled, k.Layout)
		return "xkb_keymap { fake " + k.Layout + " }", nil
	}
	f.addOutput(0, 0, 1000, 600)
	f.addSeat()
	_, kb1 := f.addKeyboard("AT Translated Set 2 keyboard")
	_, kb2 := f.addKeyboard("Apple Inc. Magic Keyboard")
	f.manageCycle()
	f.renderCycle()

	if _, err := b.runCommand([]string{"keyboard-layout", "us"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.runCommand([]string{"keyboard-layout", "-device", "*Apple*", "-options", "ctrl:nocaps", "de"}); err != nil {
		t.Fatal(err)
	}
	out, err := b.runCommand([]string{"list-inputs"})
	if err != nil || !strings.Contains(out, "Apple Inc. Magic Keyboard") || !strings.Contains(out, "(xkb)") {
		t.Fatalf("list-inputs = %q, %v", out, err)
	}
	b.Dirty()
	f.collect()

	// The manage sequence compiles both keymaps and creates them; no
	// set_keymap yet because neither has been confirmed.
	reqs := f.manageCycle()
	creates := find(reqs, "river_xkb_config_v1", xkbConfigReqCreateKeymap)
	if len(creates) != 2 {
		t.Fatalf("got %d create_keymap requests, want 2: %v", len(creates), reqs)
	}
	if len(compiled) != 2 {
		t.Fatalf("compiled %v, want 2 distinct layouts", compiled)
	}
	if got := find(reqs, "river_xkb_keyboard_v1", xkbKeyboardReqSetKeymap); len(got) != 0 {
		t.Fatalf("set_keymap sent before the keymap was confirmed")
	}
	f.renderCycle()

	// Confirm both keymaps. The bridge applies each to its waiting
	// keyboards immediately.
	keymapIDs := make([]uint32, 2)
	for i, c := range creates {
		d := c.decoder()
		keymapIDs[i], _ = d.Uint()
	}
	for _, id := range keymapIDs {
		f.server.Send(id, xkbKeymapEvSuccess, &wire.Encoder{})
	}
	f.deliverAndCollect(func() bool {
		return len(find(f.received, "river_xkb_keyboard_v1", xkbKeyboardReqSetKeymap)) >= 2
	})
	sets := find(f.received, "river_xkb_keyboard_v1", xkbKeyboardReqSetKeymap)
	if len(sets) != 2 {
		t.Fatalf("got %d set_keymap requests, want 2", len(sets))
	}
	// Each keyboard got a set_keymap, and they reference different keymaps
	// (one us, one de).
	byKb := map[uint32]uint32{}
	for _, s := range sets {
		d := s.decoder()
		km, _ := d.Object()
		byKb[s.object] = km
	}
	if byKb[kb1] == byKb[kb2] {
		t.Errorf("both keyboards got the same keymap; want distinct layouts")
	}
	if byKb[kb1] == 0 || byKb[kb2] == 0 {
		t.Errorf("a keyboard did not receive a keymap: %v", byKb)
	}

	// A subsequent manage sequence does not re-send anything.
	reqs = f.manageCycle()
	if got := find(reqs, "river_xkb_keyboard_v1", xkbKeyboardReqSetKeymap); len(got) != 0 {
		t.Errorf("set_keymap re-sent for an already-configured keyboard")
	}
	if got := find(reqs, "river_xkb_config_v1", xkbConfigReqCreateKeymap); len(got) != 0 {
		t.Errorf("create_keymap re-sent for an already-compiled layout")
	}
}

// TestKeyboardLayoutFailure checks that a keymap the compositor rejects is
// not applied and not retried every manage sequence.
func TestKeyboardLayoutFailure(t *testing.T) {
	f, b := newFakeRiver(t)
	b.CompileKeymap = func(k core.KeyboardLayout) (string, error) {
		return "xkb_keymap { bad }", nil
	}
	f.addOutput(0, 0, 1000, 600)
	f.addSeat()
	f.addKeyboard("kb")
	f.manageCycle()
	f.renderCycle()
	b.runCommand([]string{"keyboard-layout", "xx"})
	b.Dirty()
	f.collect()
	reqs := f.manageCycle()
	creates := find(reqs, "river_xkb_config_v1", xkbConfigReqCreateKeymap)
	if len(creates) != 1 {
		t.Fatalf("got %d create_keymap, want 1", len(creates))
	}
	d := creates[0].decoder()
	kmID, _ := d.Uint()
	f.renderCycle()

	e := &wire.Encoder{}
	e.PutString("unrecognized layout")
	f.server.Send(kmID, xkbKeymapEvFailure, e)
	reqs = f.manageCycle()
	if got := find(f.received, "river_xkb_keyboard_v1", xkbKeyboardReqSetKeymap); len(got) != 0 {
		t.Errorf("set_keymap sent for a failed keymap")
	}
	// No retry on the next cycle.
	reqs = f.manageCycle()
	if got := find(reqs, "river_xkb_config_v1", xkbConfigReqCreateKeymap); len(got) != 0 {
		t.Errorf("failed keymap recompiled every manage sequence")
	}
}
