package core

import (
	"fmt"
	"testing"
)

func params(mod ...func(*LayoutParams)) LayoutParams {
	p := DefaultLayoutParams()
	for _, f := range mod {
		f(&p)
	}
	return p
}

func TestTileSingleWindowFillsArea(t *testing.T) {
	area := Rect{X: 0, Y: 0, W: 1920, H: 1080}
	got := ComputeLayout(LayoutTile, area, 1, params())
	if len(got) != 1 {
		t.Fatalf("got %d rects, want 1", len(got))
	}
	if got[0] != area {
		t.Errorf("single window = %v, want %v", got[0], area)
	}
}

func TestTileTwoWindowsSplitAtRatio(t *testing.T) {
	area := Rect{X: 0, Y: 0, W: 1000, H: 600}
	got := ComputeLayout(LayoutTile, area, 2, params(func(p *LayoutParams) { p.MainRatio = 0.6 }))
	want := []Rect{
		{X: 0, Y: 0, W: 600, H: 600},
		{X: 600, Y: 0, W: 400, H: 600},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rect[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestTileStackSplitsEvenly(t *testing.T) {
	area := Rect{X: 0, Y: 0, W: 1000, H: 900}
	got := ComputeLayout(LayoutTile, area, 4, params(func(p *LayoutParams) { p.MainRatio = 0.5 }))
	// Main: 1 window at 500 wide. Stack: 3 windows at 300 tall each.
	if got[0] != (Rect{X: 0, Y: 0, W: 500, H: 900}) {
		t.Errorf("main = %v", got[0])
	}
	for i, want := range []Rect{
		{X: 500, Y: 0, W: 500, H: 300},
		{X: 500, Y: 300, W: 500, H: 300},
		{X: 500, Y: 600, W: 500, H: 300},
	} {
		if got[i+1] != want {
			t.Errorf("stack[%d] = %v, want %v", i, got[i+1], want)
		}
	}
}

func TestTileMainCount(t *testing.T) {
	area := Rect{X: 0, Y: 0, W: 1000, H: 800}
	got := ComputeLayout(LayoutTile, area, 4, params(func(p *LayoutParams) {
		p.MainRatio = 0.5
		p.MainCount = 2
	}))
	// 2 main windows split the left half vertically, 2 stack windows the right.
	if got[0] != (Rect{X: 0, Y: 0, W: 500, H: 400}) || got[1] != (Rect{X: 0, Y: 400, W: 500, H: 400}) {
		t.Errorf("main = %v, %v", got[0], got[1])
	}
	if got[2] != (Rect{X: 500, Y: 0, W: 500, H: 400}) || got[3] != (Rect{X: 500, Y: 400, W: 500, H: 400}) {
		t.Errorf("stack = %v, %v", got[2], got[3])
	}
}

func TestTileMainCountExceedsWindows(t *testing.T) {
	area := Rect{X: 0, Y: 0, W: 1000, H: 800}
	got := ComputeLayout(LayoutTile, area, 2, params(func(p *LayoutParams) { p.MainCount = 5 }))
	// All windows are main windows: they split the full area vertically.
	if got[0] != (Rect{X: 0, Y: 0, W: 1000, H: 400}) || got[1] != (Rect{X: 0, Y: 400, W: 1000, H: 400}) {
		t.Errorf("got %v", got)
	}
}

func TestTileMainLocations(t *testing.T) {
	area := Rect{X: 0, Y: 0, W: 1000, H: 800}
	cases := []struct {
		loc  MainLocation
		main Rect
	}{
		{MainLeft, Rect{X: 0, Y: 0, W: 500, H: 800}},
		{MainRight, Rect{X: 500, Y: 0, W: 500, H: 800}},
		{MainTop, Rect{X: 0, Y: 0, W: 1000, H: 400}},
		{MainBottom, Rect{X: 0, Y: 400, W: 1000, H: 400}},
	}
	for _, tc := range cases {
		t.Run(string(tc.loc), func(t *testing.T) {
			got := ComputeLayout(LayoutTile, area, 2, params(func(p *LayoutParams) {
				p.MainRatio = 0.5
				p.MainLocation = tc.loc
			}))
			if got[0] != tc.main {
				t.Errorf("main = %v, want %v", got[0], tc.main)
			}
		})
	}
}

func TestTileOffsetOutput(t *testing.T) {
	// An output positioned at x=1920 (to the right of another) must produce
	// rects in its own coordinate space, not at the origin.
	area := Rect{X: 1920, Y: 0, W: 1000, H: 600}
	got := ComputeLayout(LayoutTile, area, 2, params(func(p *LayoutParams) { p.MainRatio = 0.5 }))
	if got[0] != (Rect{X: 1920, Y: 0, W: 500, H: 600}) {
		t.Errorf("main = %v", got[0])
	}
	if got[1] != (Rect{X: 2420, Y: 0, W: 500, H: 600}) {
		t.Errorf("stack = %v", got[1])
	}
}

func TestGaps(t *testing.T) {
	area := Rect{X: 0, Y: 0, W: 1000, H: 600}
	got := ComputeLayout(LayoutTile, area, 2, params(func(p *LayoutParams) {
		p.MainRatio = 0.5
		p.InnerGap = 10
		p.OuterGap = 20
	}))
	// Usable after outer gap: 40,40 -> 960x560... wait: x=20,y=20,w=960,h=560.
	// Main gets (960-10)*0.5 = 475, stack gets 960-475-10 = 475.
	want0 := Rect{X: 20, Y: 20, W: 475, H: 560}
	want1 := Rect{X: 20 + 475 + 10, Y: 20, W: 475, H: 560}
	if got[0] != want0 {
		t.Errorf("main = %v, want %v", got[0], want0)
	}
	if got[1] != want1 {
		t.Errorf("stack = %v, want %v", got[1], want1)
	}
}

func TestSmartGapsSingleWindow(t *testing.T) {
	area := Rect{X: 0, Y: 0, W: 1000, H: 600}
	p := params(func(p *LayoutParams) {
		p.InnerGap = 10
		p.OuterGap = 20
		p.SmartGaps = true
	})
	got := ComputeLayout(LayoutTile, area, 1, p)
	if got[0] != area {
		t.Errorf("smart gaps single window = %v, want full area %v", got[0], area)
	}
	// With two windows the gaps come back.
	got = ComputeLayout(LayoutTile, area, 2, p)
	if got[0].X != 20 || got[0].Y != 20 {
		t.Errorf("smart gaps two windows: main = %v, want outer gap applied", got[0])
	}
}

func TestMonocle(t *testing.T) {
	area := Rect{X: 100, Y: 200, W: 800, H: 600}
	got := ComputeLayout(LayoutMonocle, area, 3, params())
	for i, r := range got {
		if r != area {
			t.Errorf("monocle[%d] = %v, want %v", i, r, area)
		}
	}
}

func TestRemainderPixelsAreDistributed(t *testing.T) {
	// 1000 / 3 = 333r1: the union of the stack must still exactly tile the
	// area with no gap and no overlap.
	area := Rect{X: 0, Y: 0, W: 100, H: 1000}
	got := ComputeLayout(LayoutTile, area, 3, params(func(p *LayoutParams) { p.MainCount = 3 }))
	var total int32
	for _, r := range got {
		total += r.H
	}
	if total != 1000 {
		t.Errorf("heights sum to %d, want 1000", total)
	}
	if got[2].Y+got[2].H != 1000 {
		t.Errorf("last window ends at %d, want 1000", got[2].Y+got[2].H)
	}
}

// TestLayoutInvariants sweeps a broad range of inputs and checks the
// properties every layout must uphold regardless of parameters.
func TestLayoutInvariants(t *testing.T) {
	areas := []Rect{
		{0, 0, 1920, 1080},
		{1920, 0, 2560, 1440},
		{-1280, -720, 1280, 720},
		{0, 0, 640, 480},
		{0, 0, 100, 100},
		{0, 0, 30, 30}, // pathologically small
	}
	for _, layout := range []LayoutName{LayoutTile, LayoutMonocle} {
		for _, area := range areas {
			for count := 1; count <= 10; count++ {
				for _, ratio := range []float64{0.1, 0.5, 0.9} {
					for _, mainCount := range []int{1, 2, 5} {
						for _, loc := range []MainLocation{MainLeft, MainRight, MainTop, MainBottom} {
							for _, gap := range []int32{0, 5} {
								p := LayoutParams{
									MainRatio:    ratio,
									MainCount:    mainCount,
									MainLocation: loc,
									InnerGap:     gap,
									OuterGap:     gap,
								}
								checkLayoutInvariants(t, layout, area, count, p)
							}
						}
					}
				}
			}
		}
	}
}

func checkLayoutInvariants(t *testing.T, layout LayoutName, area Rect, count int, p LayoutParams) {
	t.Helper()
	name := fmt.Sprintf("%s/%v/n=%d/%+v", layout, area, count, p)
	got := ComputeLayout(layout, area, count, p)
	if len(got) != count {
		t.Fatalf("%s: got %d rects, want %d", name, len(got), count)
	}
	for i, r := range got {
		if r.W < 1 || r.H < 1 {
			t.Errorf("%s: rect[%d] = %v has non-positive size", name, i, r)
		}
		if !area.ContainsRect(r) {
			t.Errorf("%s: rect[%d] = %v escapes area %v", name, i, r, area)
		}
	}
	// Tiled windows must not overlap (monocle windows intentionally do).
	if layout == LayoutTile && area.W >= int32(count)*2 && area.H >= int32(count)*2 {
		for i := range got {
			for j := i + 1; j < len(got); j++ {
				if got[i].Overlaps(got[j]) {
					t.Errorf("%s: rect[%d]=%v overlaps rect[%d]=%v", name, i, got[i], j, got[j])
				}
			}
		}
	}
	// Determinism.
	again := ComputeLayout(layout, area, count, p)
	for i := range got {
		if got[i] != again[i] {
			t.Errorf("%s: non-deterministic: %v then %v", name, got[i], again[i])
		}
	}
}
