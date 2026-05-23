package bridge

import (
	"os"
	"os/exec"

	"github.com/psanford/weir/core"
	"github.com/psanford/weir/protocols/river"
)

// chord identifies a key binding by what triggers it.
type chord struct {
	sym  core.Keysym
	mods core.Modifiers
}

// pointerChord identifies a pointer binding by what triggers it.
type pointerChord struct {
	button uint32
	mods   core.Modifiers
}

// keyBindingState is the bridge's bookkeeping for one protocol key binding
// object.
type keyBindingState struct {
	proxy   *river.XkbBindingV1
	enabled bool
}

// pointerBindingState is the bridge's bookkeeping for one protocol pointer
// binding object.
type pointerBindingState struct {
	proxy   *river.PointerBindingV1
	enabled bool
}

// syncBindings reconciles the model's binding set with the protocol
// objects. Called during every manage sequence: enable() and disable() may
// only be sent inside one.
func (b *Bridge) syncBindings() {
	if b.seat == nil {
		return
	}

	// Key bindings.
	if b.xkbBindings != nil {
		want := make(map[chord]bool, len(b.model.Bindings))
		for _, kb := range b.model.Bindings {
			c := chord{kb.Keysym, kb.Mods}
			want[c] = true
			if _, exists := b.keyBindings[c]; !exists {
				proxy := b.xkbBindings.GetXkbBinding(b.seat, uint32(kb.Keysym), river.SeatV1Modifiers(kb.Mods))
				st := &keyBindingState{proxy: proxy}
				b.keyBindings[c] = st
				cc := c
				proxy.OnPressed = func() { b.keyBindingPressed(cc) }
			}
			if st := b.keyBindings[c]; !st.enabled {
				st.proxy.Enable()
				st.enabled = true
			}
		}
		for c, st := range b.keyBindings {
			if !want[c] {
				st.proxy.Disable()
				st.proxy.Destroy()
				delete(b.keyBindings, c)
			}
		}
	}

	// Pointer bindings.
	want := make(map[pointerChord]bool, len(b.model.PointerBindings))
	for _, pb := range b.model.PointerBindings {
		c := pointerChord{pb.Button, pb.Mods}
		want[c] = true
		if _, exists := b.pointerBindings[c]; !exists {
			proxy := b.seat.GetPointerBinding(pb.Button, river.SeatV1Modifiers(pb.Mods))
			st := &pointerBindingState{proxy: proxy}
			b.pointerBindings[c] = st
			cc := c
			proxy.OnPressed = func() { b.pointerBindingPressed(cc) }
			proxy.OnReleased = func() { b.pointerBindingReleased(cc) }
		}
		if st := b.pointerBindings[c]; !st.enabled {
			st.proxy.Enable()
			st.enabled = true
		}
	}
	for c, st := range b.pointerBindings {
		if !want[c] {
			st.proxy.Disable()
			st.proxy.Destroy()
			delete(b.pointerBindings, c)
		}
	}
}

// keyBindingPressed runs the command bound to a chord. The compositor
// always follows a pressed event with a manage sequence, so any model
// changes the command makes are applied without an explicit manage_dirty.
func (b *Bridge) keyBindingPressed(c chord) {
	kb, ok := b.model.LookupBinding(c.sym, c.mods)
	if !ok {
		// The binding was removed but the protocol object has not been
		// destroyed yet (that happens in the next manage sequence).
		return
	}
	b.log.Debug("key binding", "chord", kb.Chord(), "command", kb.Command)
	if _, err := b.runCommand(kb.Command); err != nil {
		b.log.Warn("key binding command failed", "chord", kb.Chord(), "err", err)
	}
}

// pointerBindingPressed handles a pointer binding press: either start an
// interactive move/resize of the window under the pointer or run a command.
func (b *Bridge) pointerBindingPressed(c pointerChord) {
	pb, ok := b.model.LookupPointerBinding(c.button, c.mods)
	if !ok {
		return
	}
	switch pb.Action {
	case core.PointerActionMove, core.PointerActionResize:
		if b.pointerWindow == 0 || b.seat == nil {
			return
		}
		if b.model.StartPointerOp(b.pointerWindow, pb.Action) {
			b.opActive = true
			b.seat.OpStartPointer()
			b.log.Debug("pointer op start", "action", pb.Action, "window", b.pointerWindow)
		}
	case core.PointerActionCommand:
		// Commands bound to a pointer button act on the window that was
		// clicked, not whichever window happened to have keyboard focus:
		// focus the hovered window first.
		if b.pointerWindow != 0 {
			b.model.WindowInteracted(b.pointerWindow)
		}
		b.log.Debug("pointer binding", "chord", pb.Chord(), "command", pb.Command)
		if _, err := b.runCommand(pb.Command); err != nil {
			b.log.Warn("pointer binding command failed", "chord", pb.Chord(), "err", err)
		}
	}
}

// pointerBindingReleased ends an interactive op when the triggering button
// is released. The op_release event also fires; ending on either is
// harmless because EndPointerOp and OpEnd are idempotent.
func (b *Bridge) pointerBindingReleased(c pointerChord) {
	pb, ok := b.model.LookupPointerBinding(c.button, c.mods)
	if !ok || (pb.Action != core.PointerActionMove && pb.Action != core.PointerActionResize) {
		return
	}
	b.endPointerOp()
}

// endPointerOp finishes the interactive op on both the model and protocol
// sides.
func (b *Bridge) endPointerOp() {
	if !b.opActive {
		return
	}
	b.opActive = false
	b.model.EndPointerOp()
	if b.seat != nil {
		b.seat.OpEnd()
	}
	b.log.Debug("pointer op end")
}

// drainSideEffects executes side effects queued by commands: spawned
// processes. Called after every command dispatch, whether it came from the
// IPC socket or a key binding.
func (b *Bridge) drainSideEffects() {
	for _, line := range b.model.SpawnRequests {
		b.spawn(line)
	}
	b.model.SpawnRequests = b.model.SpawnRequests[:0]
}

// spawn runs a shell command as a detached child. The child inherits
// weir's environment (including WAYLAND_DISPLAY) but not its stdio.
func (b *Bridge) spawn(line string) {
	cmd := exec.Command("/bin/sh", "-c", line)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		b.log.Warn("spawn failed", "command", line, "err", err)
		return
	}
	b.log.Debug("spawned", "command", line, "pid", cmd.Process.Pid)
	// Reap the child when it exits so it does not become a zombie.
	go cmd.Wait()
}
