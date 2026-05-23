package core

import (
	"fmt"
	"sort"
	"strings"
)

// InputDeviceID identifies an input device. Assigned by the bridge.
type InputDeviceID uint64

// InputDevice is the model's view of a physical input device.
type InputDevice struct {
	ID   InputDeviceID
	Name string
	// Type is keyboard|pointer|touch|tablet, or "" until reported.
	Type string
	// XkbKeyboard is true if the compositor manages this device's state
	// with xkbcommon, i.e. a keymap can be assigned to it.
	XkbKeyboard bool
}

// KeyboardLayout is a desired xkb keymap configuration for one or more
// keyboards. The zero value of the RMLVO fields means "use the xkb
// default".
type KeyboardLayout struct {
	// Device is a glob matched against device names; empty matches every
	// keyboard.
	Device string
	// Rules, Model, Layout, Variant, Options are the xkb RMLVO names
	// passed to the keymap compiler.
	Rules   string
	Model   string
	Layout  string
	Variant string
	Options string
}

// RMLVO returns a stable string identifying the compiled keymap, used as a
// cache key by the bridge.
func (k KeyboardLayout) RMLVO() string {
	return strings.Join([]string{k.Rules, k.Model, k.Layout, k.Variant, k.Options}, "\x00")
}

func (k KeyboardLayout) String() string {
	var b strings.Builder
	if k.Device != "" {
		fmt.Fprintf(&b, "-device %q ", k.Device)
	}
	for _, f := range []struct{ flag, val string }{
		{"-rules", k.Rules}, {"-model", k.Model}, {"-variant", k.Variant}, {"-options", k.Options},
	} {
		if f.val != "" {
			fmt.Fprintf(&b, "%s %s ", f.flag, f.val)
		}
	}
	b.WriteString(k.Layout)
	return strings.TrimSpace(b.String())
}

// LayoutForDevice returns the keyboard layout that applies to the named
// device: the last configured layout whose device glob matches. ok is false
// if no layout matches.
func (m *Model) LayoutForDevice(name string) (KeyboardLayout, bool) {
	for i := len(m.KeyboardLayouts) - 1; i >= 0; i-- {
		if globMatch(m.KeyboardLayouts[i].Device, name) {
			return m.KeyboardLayouts[i], true
		}
	}
	return KeyboardLayout{}, false
}

// ---------------------------------------------------------------------------
// Input device events
// ---------------------------------------------------------------------------

// InputDeviceAdded records a new input device.
func (m *Model) InputDeviceAdded(id InputDeviceID) {
	if _, exists := m.InputDevices[id]; exists {
		return
	}
	m.InputDevices[id] = &InputDevice{ID: id}
}

// InputDeviceName records a device's name.
func (m *Model) InputDeviceName(id InputDeviceID, name string) {
	if d, ok := m.InputDevices[id]; ok {
		d.Name = name
	}
}

// InputDeviceType records a device's type.
func (m *Model) InputDeviceType(id InputDeviceID, typ string) {
	if d, ok := m.InputDevices[id]; ok {
		d.Type = typ
	}
}

// InputDeviceXkb marks a device as an xkbcommon-managed keyboard.
func (m *Model) InputDeviceXkb(id InputDeviceID) {
	if d, ok := m.InputDevices[id]; ok {
		d.XkbKeyboard = true
	}
}

// InputDeviceRemoved removes an input device.
func (m *Model) InputDeviceRemoved(id InputDeviceID) {
	delete(m.InputDevices, id)
}

// ---------------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------------

// cmdListInputs implements: list-inputs
func cmdListInputs(m *Model, _ []string) (string, error) {
	ids := make([]InputDeviceID, 0, len(m.InputDevices))
	for id := range m.InputDevices {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var lines []string
	for _, id := range ids {
		d := m.InputDevices[id]
		typ := d.Type
		if typ == "" {
			typ = "unknown"
		}
		if d.XkbKeyboard {
			typ += " (xkb)"
		}
		lines = append(lines, fmt.Sprintf("%-12s %s", typ, d.Name))
	}
	return strings.Join(lines, "\n"), nil
}

// cmdKeyboardLayout implements:
// keyboard-layout [-rules R] [-model M] [-variant V] [-options O] [-device <glob>] <layout>
func cmdKeyboardLayout(m *Model, args []string) (string, error) {
	var k KeyboardLayout
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		if len(args) < 2 {
			return "", cmdErrf("%s requires a value", args[0])
		}
		switch args[0] {
		case "-rules":
			k.Rules = args[1]
		case "-model":
			k.Model = args[1]
		case "-variant":
			k.Variant = args[1]
		case "-options":
			k.Options = args[1]
		case "-device":
			if _, err := globMatchErr(args[1]); err != nil {
				return "", cmdErrf("invalid device glob %q: %v", args[1], err)
			}
			k.Device = args[1]
		default:
			return "", cmdErrf("unknown option %q (want -rules|-model|-variant|-options|-device)", args[0])
		}
		args = args[2:]
	}
	if len(args) != 1 || args[0] == "" {
		return "", cmdErrf("usage: keyboard-layout [-rules R] [-model M] [-variant V] [-options O] [-device <glob>] <layout>")
	}
	k.Layout = args[0]
	// Replace any existing entry for the same device glob so repeated
	// configuration converges instead of accumulating.
	for i := range m.KeyboardLayouts {
		if m.KeyboardLayouts[i].Device == k.Device {
			m.KeyboardLayouts[i] = k
			m.markChanged()
			return "", nil
		}
	}
	m.KeyboardLayouts = append(m.KeyboardLayouts, k)
	m.markChanged()
	return "", nil
}

// globMatchErr validates a glob pattern.
func globMatchErr(pattern string) (bool, error) {
	return true, validateGlob(pattern)
}
