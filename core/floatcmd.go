package core

import "strconv"

// floatFocused returns the focused window, converting it to floating at its
// current geometry first if it is tiled. Returns nil if there is no focused
// window or it is fullscreen.
func (m *Model) floatFocused() *Window {
	w := m.FocusedWindow()
	if w == nil || w.FullscreenOn != 0 {
		return nil
	}
	if !w.Floating {
		// Adopt the current tiled geometry so the transition is seamless,
		// matching the interactive pointer move behavior.
		arr := m.Arrange()
		if p, ok := arr.Placements[w.ID]; ok && !p.Rect.Empty() {
			w.FloatRect = p.Rect
		}
		m.setFloating(w, true)
	}
	return w
}

// cmdMove implements: move left|right|up|down <px>
// The focused window becomes floating and shifts in the given direction.
func cmdMove(m *Model, args []string) (string, error) {
	if len(args) != 2 {
		return "", cmdErrf("usage: move left|right|up|down <px>")
	}
	dx, dy, err := direction(args[0], args[1])
	if err != nil {
		return "", err
	}
	w := m.floatFocused()
	if w == nil {
		return "", nil
	}
	w.FloatRect.X += dx
	w.FloatRect.Y += dy
	m.markChanged()
	return "", nil
}

// cmdSnap implements: snap left|right|up|down
// The focused window becomes floating and moves flush against the given
// edge of the output's usable area.
func cmdSnap(m *Model, args []string) (string, error) {
	if len(args) != 1 {
		return "", cmdErrf("usage: snap left|right|up|down")
	}
	w := m.floatFocused()
	if w == nil {
		return "", nil
	}
	area := m.outputAreaForWorkspace(w.Workspace)
	switch args[0] {
	case "left":
		w.FloatRect.X = area.X
	case "right":
		w.FloatRect.X = area.X + area.W - w.FloatRect.W
	case "up":
		w.FloatRect.Y = area.Y
	case "down":
		w.FloatRect.Y = area.Y + area.H - w.FloatRect.H
	default:
		return "", cmdErrf("usage: snap left|right|up|down")
	}
	m.markChanged()
	return "", nil
}

// cmdResize implements: resize horizontal|vertical <±px>
// The focused window becomes floating and grows or shrinks along the given
// axis, keeping its center fixed and respecting the client's size hints.
func cmdResize(m *Model, args []string) (string, error) {
	if len(args) != 2 {
		return "", cmdErrf("usage: resize horizontal|vertical <px>")
	}
	delta, err := strconv.ParseInt(args[1], 10, 32)
	if err != nil {
		return "", cmdErrf("invalid size delta %q", args[1])
	}
	w := m.floatFocused()
	if w == nil {
		return "", nil
	}
	r := w.FloatRect
	switch args[0] {
	case "horizontal":
		minW := int32(minResizeW)
		if w.MinW > 0 {
			minW = w.MinW
		}
		newW := max32(r.W+int32(delta), minW)
		if w.MaxW > 0 && newW > w.MaxW {
			newW = w.MaxW
		}
		r.X -= (newW - r.W) / 2
		r.W = newW
	case "vertical":
		minH := int32(minResizeH)
		if w.MinH > 0 {
			minH = w.MinH
		}
		newH := max32(r.H+int32(delta), minH)
		if w.MaxH > 0 && newH > w.MaxH {
			newH = w.MaxH
		}
		r.Y -= (newH - r.H) / 2
		r.H = newH
	default:
		return "", cmdErrf("usage: resize horizontal|vertical <px>")
	}
	w.FloatRect = r
	m.markChanged()
	return "", nil
}

// direction maps a direction name and a pixel count to a (dx, dy) offset.
func direction(dir, px string) (int32, int32, error) {
	n, err := strconv.ParseInt(px, 10, 32)
	if err != nil || n < 0 {
		return 0, 0, cmdErrf("invalid distance %q", px)
	}
	d := int32(n)
	switch dir {
	case "left":
		return -d, 0, nil
	case "right":
		return d, 0, nil
	case "up":
		return 0, -d, nil
	case "down":
		return 0, d, nil
	}
	return 0, 0, cmdErrf("invalid direction %q (want left|right|up|down)", dir)
}
