package core

import "fmt"

// MainLocation is the edge of the output the main area is attached to,
// following rivercarro's main-location option.
type MainLocation string

const (
	MainLeft   MainLocation = "left"
	MainRight  MainLocation = "right"
	MainTop    MainLocation = "top"
	MainBottom MainLocation = "bottom"
)

// LayoutName identifies a layout algorithm.
type LayoutName string

const (
	LayoutTile    LayoutName = "tile"
	LayoutMonocle LayoutName = "monocle"
)

// LayoutParams are the per-workspace knobs that parameterize a layout.
// Every field is adjustable at runtime via the command interface.
type LayoutParams struct {
	// MainRatio is the fraction of the output occupied by the main area,
	// clamped to [MinMainRatio, MaxMainRatio].
	MainRatio float64
	// MainCount is the number of windows in the main area, >= 1.
	MainCount int
	// MainLocation is the edge the main area is attached to.
	MainLocation MainLocation
	// InnerGap is the gap in pixels between adjacent windows.
	InnerGap int32
	// OuterGap is the gap in pixels between windows and the output edge.
	OuterGap int32
	// SmartGaps disables all gaps when the layout would place only one
	// window on the output.
	SmartGaps bool
}

const (
	MinMainRatio = 0.1
	MaxMainRatio = 0.9
)

// DefaultLayoutParams returns the parameters new workspaces start with.
func DefaultLayoutParams() LayoutParams {
	return LayoutParams{
		MainRatio:    0.6,
		MainCount:    1,
		MainLocation: MainLeft,
		InnerGap:     0,
		OuterGap:     0,
		SmartGaps:    false,
	}
}

// Clamp normalizes the parameters into their valid ranges.
func (p LayoutParams) Clamp() LayoutParams {
	if p.MainRatio < MinMainRatio {
		p.MainRatio = MinMainRatio
	}
	if p.MainRatio > MaxMainRatio {
		p.MainRatio = MaxMainRatio
	}
	if p.MainCount < 1 {
		p.MainCount = 1
	}
	if p.InnerGap < 0 {
		p.InnerGap = 0
	}
	if p.OuterGap < 0 {
		p.OuterGap = 0
	}
	switch p.MainLocation {
	case MainLeft, MainRight, MainTop, MainBottom:
	default:
		p.MainLocation = MainLeft
	}
	return p
}

// ComputeLayout computes the geometry for count tiled windows within area
// using the named layout. The returned slice always has exactly count
// entries. Index 0 is the first window in the workspace stack (the first
// main window).
//
// ComputeLayout is a pure function: it depends only on its arguments, never
// on model state, which is what makes layouts trivially unit-testable.
func ComputeLayout(layout LayoutName, area Rect, count int, p LayoutParams) []Rect {
	if count <= 0 {
		return nil
	}
	p = p.Clamp()
	switch layout {
	case LayoutMonocle:
		return arrangeMonocle(area, count, p)
	default:
		return arrangeTile(area, count, p)
	}
}

// ValidLayout reports whether name is a known layout.
func ValidLayout(name LayoutName) bool {
	switch name {
	case LayoutTile, LayoutMonocle:
		return true
	}
	return false
}

// arrangeMonocle gives every window the full usable area.
func arrangeMonocle(area Rect, count int, p LayoutParams) []Rect {
	usable := area
	if !p.SmartGaps {
		usable = applyOuterGap(area, p.OuterGap)
	}
	rects := make([]Rect, count)
	for i := range rects {
		rects[i] = usable
	}
	return rects
}

// arrangeTile implements the classic master/stack layout: MainCount windows
// split the main area evenly along the secondary axis, the remaining windows
// split the stack area evenly along the secondary axis.
func arrangeTile(area Rect, count int, p LayoutParams) []Rect {
	smart := p.SmartGaps && count == 1
	outer := p.OuterGap
	inner := p.InnerGap
	if smart {
		outer = 0
		inner = 0
	}
	usable := applyOuterGap(area, outer)

	nMain := p.MainCount
	if nMain > count {
		nMain = count
	}
	nStack := count - nMain

	if nStack == 0 {
		// Only the main column exists; it takes the full usable area.
		return splitEven(usable, nMain, secondaryAxis(p.MainLocation), inner)
	}

	mainArea, stackArea := splitMain(usable, p.MainLocation, p.MainRatio, inner)
	rects := make([]Rect, 0, count)
	rects = append(rects, splitEven(mainArea, nMain, secondaryAxis(p.MainLocation), inner)...)
	rects = append(rects, splitEven(stackArea, nStack, secondaryAxis(p.MainLocation), inner)...)
	return rects
}

type axis int

const (
	axisX axis = iota
	axisY
)

// secondaryAxis returns the axis along which windows within the main or
// stack area are stacked. A left/right main area stacks its windows
// vertically; a top/bottom main area stacks them horizontally.
func secondaryAxis(loc MainLocation) axis {
	switch loc {
	case MainTop, MainBottom:
		return axisX
	default:
		return axisY
	}
}

// applyOuterGap shrinks area by gap on all edges.
func applyOuterGap(area Rect, gap int32) Rect {
	if gap <= 0 {
		return area
	}
	return area.Inset(gap)
}

// splitMain divides usable into a main area and a stack area separated by
// gap, with the main area attached to the edge given by loc and sized by
// ratio along the primary axis.
func splitMain(usable Rect, loc MainLocation, ratio float64, gap int32) (main, stack Rect) {
	switch loc {
	case MainTop, MainBottom:
		mainH := proportion(usable.H, gap, ratio)
		stackH := usable.H - mainH - gap
		if loc == MainTop {
			main = Rect{X: usable.X, Y: usable.Y, W: usable.W, H: mainH}
			stack = Rect{X: usable.X, Y: usable.Y + mainH + gap, W: usable.W, H: stackH}
		} else {
			stack = Rect{X: usable.X, Y: usable.Y, W: usable.W, H: stackH}
			main = Rect{X: usable.X, Y: usable.Y + stackH + gap, W: usable.W, H: mainH}
		}
	default: // left, right
		mainW := proportion(usable.W, gap, ratio)
		stackW := usable.W - mainW - gap
		if loc == MainLeft {
			main = Rect{X: usable.X, Y: usable.Y, W: mainW, H: usable.H}
			stack = Rect{X: usable.X + mainW + gap, Y: usable.Y, W: stackW, H: usable.H}
		} else {
			stack = Rect{X: usable.X, Y: usable.Y, W: stackW, H: usable.H}
			main = Rect{X: usable.X + stackW + gap, Y: usable.Y, W: mainW, H: usable.H}
		}
	}
	return main, stack
}

// proportion returns the size of the main area given the total size, the gap
// separating main from stack, and the ratio. The result is always at least 1
// and leaves at least 1 pixel for the stack.
func proportion(total, gap int32, ratio float64) int32 {
	avail := total - gap
	if avail < 2 {
		// Pathologically small area: give main everything we can.
		return max32(total/2, 1)
	}
	n := int32(float64(avail) * ratio)
	if n < 1 {
		n = 1
	}
	if n > avail-1 {
		n = avail - 1
	}
	return n
}

// splitEven divides area into n rectangles of equal size along the given
// axis, separated by gap. Remainder pixels are distributed one each to the
// first rectangles so the union always exactly covers area.
func splitEven(area Rect, n int, along axis, gap int32) []Rect {
	if n <= 0 {
		return nil
	}
	if n == 1 {
		return []Rect{area}
	}
	rects := make([]Rect, n)
	totalGap := gap * int32(n-1)
	switch along {
	case axisY:
		avail := area.H - totalGap
		if avail < int32(n) {
			// Not enough room to give every window a pixel; degrade to
			// overlapping 1-px-tall slots rather than producing zero or
			// negative heights.
			for i := range rects {
				rects[i] = Rect{X: area.X, Y: area.Y + int32(i)%max32(area.H, 1), W: area.W, H: 1}
			}
			return rects
		}
		each := avail / int32(n)
		rem := avail % int32(n)
		y := area.Y
		for i := range rects {
			h := each
			if int32(i) < rem {
				h++
			}
			rects[i] = Rect{X: area.X, Y: y, W: area.W, H: h}
			y += h + gap
		}
	case axisX:
		avail := area.W - totalGap
		if avail < int32(n) {
			for i := range rects {
				rects[i] = Rect{X: area.X + int32(i)%max32(area.W, 1), Y: area.Y, W: 1, H: area.H}
			}
			return rects
		}
		each := avail / int32(n)
		rem := avail % int32(n)
		x := area.X
		for i := range rects {
			w := each
			if int32(i) < rem {
				w++
			}
			rects[i] = Rect{X: x, Y: area.Y, W: w, H: area.H}
			x += w + gap
		}
	}
	return rects
}

func max32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

// ParseMainLocation parses a main-location argument.
func ParseMainLocation(s string) (MainLocation, error) {
	switch MainLocation(s) {
	case MainLeft, MainRight, MainTop, MainBottom:
		return MainLocation(s), nil
	}
	return "", fmt.Errorf("invalid main location %q (want left|right|top|bottom)", s)
}
