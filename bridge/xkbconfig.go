package bridge

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/psanford/weir/core"
	"github.com/psanford/weir/protocols/river"
)

// inputDeviceState is the bridge's bookkeeping for one input device.
type inputDeviceState struct {
	id    core.InputDeviceID
	proxy *river.InputDeviceV1
}

// xkbKeyboardState is the bridge's bookkeeping for one xkbcommon keyboard.
type xkbKeyboardState struct {
	proxy *river.XkbKeyboardV1
	// device is the core ID of the corresponding input device, or 0 until
	// the input_device event arrives.
	device core.InputDeviceID
	// appliedRMLVO is the cache key of the keymap currently set on this
	// keyboard, or "" if weir has never set one.
	appliedRMLVO string
}

// keymapState tracks an in-flight or completed keymap compilation.
type keymapState struct {
	proxy *river.XkbKeymapV1
	rmlvo string
	// state is pending until the compositor reports success or failure.
	state keymapResult
	// waiting are the keyboards to apply this keymap to once it is ready.
	waiting []*xkbKeyboardState
	// file holds the keymap fd. It must stay referenced until the
	// compositor has answered (the finalizer would otherwise close the fd
	// before it is sent), and is closed once it has.
	file *os.File
}

type keymapResult int

const (
	keymapPending keymapResult = iota
	keymapReady
	keymapFailed
)

// installInputHandlers wires up the input manager and xkb config globals.
// Called from Bootstrap after the globals are bound.
func (b *Bridge) installInputHandlers() {
	if b.inputManager != nil {
		b.inputManager.OnInputDevice = func(d *river.InputDeviceV1) { b.addInputDevice(d) }
	}
	if b.xkbConfig != nil {
		b.xkbConfig.OnXkbKeyboard = func(k *river.XkbKeyboardV1) { b.addXkbKeyboard(k) }
	}
}

func (b *Bridge) addInputDevice(d *river.InputDeviceV1) {
	b.nextInputID++
	st := &inputDeviceState{id: b.nextInputID, proxy: d}
	b.inputDevices[st.id] = st
	b.model.InputDeviceAdded(st.id)
	d.OnName = func(name string) {
		b.log.Debug("input device", "id", st.id, "name", name)
		b.model.InputDeviceName(st.id, name)
	}
	d.OnType = func(t river.InputDeviceV1Type) {
		b.model.InputDeviceType(st.id, inputTypeName(t))
	}
	d.OnRemoved = func() {
		b.model.InputDeviceRemoved(st.id)
		delete(b.inputDevices, st.id)
		d.Destroy()
	}
}

func inputTypeName(t river.InputDeviceV1Type) string {
	switch t {
	case river.InputDeviceV1TypeKeyboard:
		return "keyboard"
	case river.InputDeviceV1TypePointer:
		return "pointer"
	case river.InputDeviceV1TypeTouch:
		return "touch"
	case river.InputDeviceV1TypeTablet:
		return "tablet"
	}
	return fmt.Sprintf("type-%d", t)
}

func (b *Bridge) addXkbKeyboard(k *river.XkbKeyboardV1) {
	st := &xkbKeyboardState{proxy: k}
	b.xkbKeyboards[k] = st
	k.OnInputDevice = func(dev *river.InputDeviceV1) {
		for id, ds := range b.inputDevices {
			if ds.proxy == dev {
				st.device = id
				b.model.InputDeviceXkb(id)
				break
			}
		}
	}
	k.OnRemoved = func() {
		delete(b.xkbKeyboards, k)
		k.Destroy()
	}
}

// syncKeyboardLayouts applies the model's desired keyboard layouts to every
// xkb keyboard whose current keymap differs. Compilation and the
// create_keymap round trip are asynchronous: keyboards wait on the keymap's
// success event.
func (b *Bridge) syncKeyboardLayouts() {
	if b.xkbConfig == nil {
		return
	}
	for _, kb := range b.xkbKeyboards {
		dev, ok := b.model.InputDevices[kb.device]
		if !ok {
			continue
		}
		layout, ok := b.model.LayoutForDevice(dev.Name)
		if !ok {
			continue
		}
		if kb.appliedRMLVO == layout.RMLVO() {
			continue
		}
		km := b.keymapFor(layout)
		switch km.state {
		case keymapReady:
			kb.proxy.SetKeymap(km.proxy)
			kb.appliedRMLVO = km.rmlvo
			b.log.Info("keymap applied", "device", dev.Name, "layout", layout.String())
		case keymapPending:
			km.waiting = appendUnique(km.waiting, kb)
		case keymapFailed:
			// Already logged; do not retry every manage sequence.
			kb.appliedRMLVO = km.rmlvo
		}
	}
}

func appendUnique(s []*xkbKeyboardState, k *xkbKeyboardState) []*xkbKeyboardState {
	for _, e := range s {
		if e == k {
			return s
		}
	}
	return append(s, k)
}

// keymapFor returns the keymap object for a layout, starting compilation
// and creation if it has not been requested before.
func (b *Bridge) keymapFor(layout core.KeyboardLayout) *keymapState {
	if km, ok := b.keymaps[layout.RMLVO()]; ok {
		return km
	}
	km := &keymapState{rmlvo: layout.RMLVO(), state: keymapPending}
	b.keymaps[layout.RMLVO()] = km

	text, err := b.compileKeymap(layout)
	if err != nil {
		b.log.Error("keymap compilation failed", "layout", layout.String(), "err", err)
		km.state = keymapFailed
		return km
	}
	f, err := keymapFd(text)
	if err != nil {
		b.log.Error("creating keymap fd", "err", err)
		km.state = keymapFailed
		return km
	}
	km.file = f
	km.proxy = b.xkbConfig.CreateKeymap(int(f.Fd()), river.XkbConfigV1KeymapFormatTextV1)
	km.proxy.OnSuccess = func() {
		km.state = keymapReady
		closeKeymapFd(km)
		b.log.Debug("keymap compiled and accepted", "layout", layout.String())
		for _, kb := range km.waiting {
			if _, live := b.xkbKeyboards[kb.proxy]; !live {
				continue
			}
			kb.proxy.SetKeymap(km.proxy)
			kb.appliedRMLVO = km.rmlvo
		}
		km.waiting = nil
		b.conn.Flush()
	}
	km.proxy.OnFailure = func(msg string) {
		km.state = keymapFailed
		closeKeymapFd(km)
		b.log.Error("compositor rejected keymap", "layout", layout.String(), "err", msg)
		for _, kb := range km.waiting {
			kb.appliedRMLVO = km.rmlvo
		}
		km.waiting = nil
	}
	return km
}

func closeKeymapFd(km *keymapState) {
	if km.file != nil {
		km.file.Close()
		km.file = nil
	}
}

// compileKeymap turns RMLVO names into XKB keymap text. The default
// implementation shells out to xkbcli (part of libxkbcommon); tests
// override CompileKeymap to avoid the dependency.
func (b *Bridge) compileKeymap(layout core.KeyboardLayout) (string, error) {
	if b.CompileKeymap != nil {
		return b.CompileKeymap(layout)
	}
	args := []string{"compile-keymap", "--layout", layout.Layout}
	if layout.Rules != "" {
		args = append(args, "--rules", layout.Rules)
	}
	if layout.Model != "" {
		args = append(args, "--model", layout.Model)
	}
	if layout.Variant != "" {
		args = append(args, "--variant", layout.Variant)
	}
	if layout.Options != "" {
		args = append(args, "--options", layout.Options)
	}
	cmd := exec.Command("xkbcli", args...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("xkbcli: %s", ee.Stderr)
		}
		return "", fmt.Errorf("running xkbcli (is libxkbcommon installed?): %w", err)
	}
	if len(out) == 0 {
		return "", fmt.Errorf("xkbcli produced an empty keymap")
	}
	return string(out), nil
}

// keymapFd writes the keymap text to an unlinked temporary file and returns
// it. The compositor mmaps the fd; the caller must keep the file referenced
// until the fd has been sent and close it afterwards.
func keymapFd(text string) (*os.File, error) {
	f, err := os.CreateTemp("", "weir-keymap-*")
	if err != nil {
		return nil, err
	}
	// Unlink immediately: the fd keeps the inode alive and nothing else
	// needs the path.
	os.Remove(f.Name())
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
