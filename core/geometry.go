package core

import "fmt"

// Rect is a rectangle in the compositor's logical coordinate space.
// Wayland uses int32 for all coordinates; we match it to avoid conversions
// at the protocol boundary.
type Rect struct {
	X, Y, W, H int32
}

func (r Rect) String() string {
	return fmt.Sprintf("%dx%d%+d%+d", r.W, r.H, r.X, r.Y)
}

// Empty reports whether the rectangle has no area.
func (r Rect) Empty() bool {
	return r.W <= 0 || r.H <= 0
}

// Contains reports whether the point (x, y) is inside the rectangle.
func (r Rect) Contains(x, y int32) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// Overlaps reports whether r and other share any area.
func (r Rect) Overlaps(other Rect) bool {
	if r.Empty() || other.Empty() {
		return false
	}
	return r.X < other.X+other.W && other.X < r.X+r.W &&
		r.Y < other.Y+other.H && other.Y < r.Y+r.H
}

// ContainsRect reports whether other lies entirely within r.
func (r Rect) ContainsRect(other Rect) bool {
	if other.Empty() {
		return false
	}
	return other.X >= r.X && other.Y >= r.Y &&
		other.X+other.W <= r.X+r.W && other.Y+other.H <= r.Y+r.H
}

// CenterIn returns a rectangle of size w x h centered within r.
// If w or h exceed r's size the result is clamped to r's origin on that axis
// so the top-left corner (and any close button living there) stays reachable.
func (r Rect) CenterIn(w, h int32) Rect {
	x := r.X + (r.W-w)/2
	y := r.Y + (r.H-h)/2
	if w > r.W {
		x = r.X
	}
	if h > r.H {
		y = r.Y
	}
	return Rect{X: x, Y: y, W: w, H: h}
}

// Inset returns r shrunk by n on every edge. If the result would be empty,
// the rectangle collapses toward its center rather than going negative.
func (r Rect) Inset(n int32) Rect {
	if 2*n >= r.W || 2*n >= r.H {
		// Degenerate: keep a 1x1 rect at the center so callers never see
		// a negative size.
		return Rect{X: r.X + r.W/2, Y: r.Y + r.H/2, W: 1, H: 1}
	}
	return Rect{X: r.X + n, Y: r.Y + n, W: r.W - 2*n, H: r.H - 2*n}
}

// Center returns the center point of the rectangle.
func (r Rect) Center() (int32, int32) {
	return r.X + r.W/2, r.Y + r.H/2
}
