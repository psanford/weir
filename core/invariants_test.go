package core

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// TestRandomOperationSequences drives the model with thousands of random
// operation sequences and checks every invariant from PLAN.md after each
// step. On failure it prints the full operation log so the sequence can be
// replayed by hand (or turned into a regression test).
//
// The generator is seeded deterministically so failures are reproducible;
// crank iterations up locally or in CI for deeper soaks.
func TestRandomOperationSequences(t *testing.T) {
	const (
		seeds        = 200
		opsPerSeed   = 250
		printLastOps = 40
	)
	for seed := int64(0); seed < seeds; seed++ {
		seed := seed
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			t.Parallel()
			rng := rand.New(rand.NewSource(seed))
			m := NewModel()
			g := &opGenerator{rng: rng}
			var log []string
			for i := 0; i < opsPerSeed; i++ {
				op := g.next(m)
				log = append(log, op.desc)
				op.apply(m)
				if err := checkInvariants(m); err != nil {
					start := len(log) - printLastOps
					if start < 0 {
						start = 0
					}
					t.Fatalf("invariant violated after op %d: %v\nlast ops:\n  %s",
						i, err, strings.Join(log[start:], "\n  "))
				}
			}
		})
	}
}

type op struct {
	desc  string
	apply func(*Model)
}

// opGenerator produces random but plausible operations: it tracks which IDs
// it has handed out so removals and commands usually reference live objects,
// but it also occasionally targets bogus IDs to exercise the "ignore unknown
// object" paths.
type opGenerator struct {
	rng        *rand.Rand
	nextWindow WindowID
	nextOutput OutputID
}

func (g *opGenerator) next(m *Model) op {
	// Weighted choice of operation kind.
	type weighted struct {
		weight int
		gen    func(*Model) op
	}
	choices := []weighted{
		{12, g.addWindow},
		{6, g.closeWindow},
		{3, g.addOutput},
		{2, g.removeOutput},
		{1, g.resizeOutput},
		{4, g.setParent},
		{3, g.reportDimensions},
		{2, g.interact},
		{30, g.command},
	}
	total := 0
	for _, c := range choices {
		total += c.weight
	}
	n := g.rng.Intn(total)
	for _, c := range choices {
		if n < c.weight {
			return c.gen(m)
		}
		n -= c.weight
	}
	panic("unreachable")
}

func (g *opGenerator) addWindow(_ *Model) op {
	g.nextWindow++
	id := g.nextWindow
	return op{
		desc:  fmt.Sprintf("WindowAdded(%d)", id),
		apply: func(m *Model) { m.WindowAdded(id) },
	}
}

func (g *opGenerator) closeWindow(m *Model) op {
	id := g.pickWindow(m)
	return op{
		desc:  fmt.Sprintf("WindowClosed(%d)", id),
		apply: func(m *Model) { m.WindowClosed(id) },
	}
}

func (g *opGenerator) addOutput(_ *Model) op {
	g.nextOutput++
	id := g.nextOutput
	// Outputs are placed side by side; geometry overlap is prevented by the
	// compositor in reality, so the generator does not produce it.
	rect := Rect{X: int32(id) * 1920, Y: 0, W: 1920, H: 1080}
	name := fmt.Sprintf("OUT-%d", id)
	return op{
		desc:  fmt.Sprintf("OutputAdded(%d, %s, %v)", id, name, rect),
		apply: func(m *Model) { m.OutputAdded(id, name, rect) },
	}
}

func (g *opGenerator) removeOutput(m *Model) op {
	id := g.pickOutput(m)
	return op{
		desc:  fmt.Sprintf("OutputRemoved(%d)", id),
		apply: func(m *Model) { m.OutputRemoved(id) },
	}
}

func (g *opGenerator) resizeOutput(m *Model) op {
	id := g.pickOutput(m)
	rect := Rect{X: int32(id) * 1920, Y: 0, W: int32(800 + g.rng.Intn(3000)), H: int32(600 + g.rng.Intn(2000))}
	return op{
		desc:  fmt.Sprintf("OutputGeometry(%d, %v)", id, rect),
		apply: func(m *Model) { m.OutputGeometry(id, rect) },
	}
}

func (g *opGenerator) setParent(m *Model) op {
	child := g.pickWindow(m)
	parent := g.pickWindow(m)
	if parent == child {
		parent = 0
	}
	return op{
		desc:  fmt.Sprintf("WindowParent(%d, %d)", child, parent),
		apply: func(m *Model) { m.WindowParent(child, parent) },
	}
}

func (g *opGenerator) reportDimensions(m *Model) op {
	id := g.pickWindow(m)
	w, h := int32(100+g.rng.Intn(2000)), int32(100+g.rng.Intn(1500))
	return op{
		desc:  fmt.Sprintf("WindowDimensions(%d, %d, %d)", id, w, h),
		apply: func(m *Model) { m.WindowDimensions(id, w, h) },
	}
}

func (g *opGenerator) interact(m *Model) op {
	id := g.pickWindow(m)
	return op{
		desc:  fmt.Sprintf("WindowInteracted(%d)", id),
		apply: func(m *Model) { m.WindowInteracted(id) },
	}
}

var commandPool = [][]string{
	{"focus", "next"}, {"focus", "prev"}, {"focus", "main"},
	{"swap", "next"}, {"swap", "prev"}, {"swap", "main"},
	{"zoom"},
	{"close"},
	{"view", "1"}, {"view", "2"}, {"view", "3"}, {"view", "9"}, {"view", "scratch"},
	{"pull", "1"}, {"pull", "2"}, {"pull", "5"},
	{"send", "1"}, {"send", "2"}, {"send", "4"}, {"send", "scratch"},
	{"focus-output", "next"}, {"focus-output", "prev"},
	{"focus-output", "left"}, {"focus-output", "right"},
	{"send-to-output", "next"}, {"send-to-output", "left"}, {"send-to-output", "right"},
	{"set-layout", "tile"}, {"set-layout", "monocle"},
	{"set", "main-ratio", "+0.05"}, {"set", "main-ratio", "-0.05"},
	{"set", "main-count", "+1"}, {"set", "main-count", "-1"},
	{"set", "main-location", "left"}, {"set", "main-location", "top"},
	{"set", "gaps", "5", "10"}, {"set", "gaps", "0", "0"},
	{"set", "smart-gaps", "on"},
	{"toggle-float"},
	{"toggle-fullscreen"},
	{"workspace-mode", "locked"}, {"workspace-mode", "independent"},
}

func (g *opGenerator) command(_ *Model) op {
	args := commandPool[g.rng.Intn(len(commandPool))]
	return op{
		desc: "Dispatch(" + strings.Join(args, " ") + ")",
		apply: func(m *Model) {
			// User errors (no outputs, etc.) are expected; internal errors
			// are not, but invariant checks after the op will catch any
			// resulting corruption.
			_, _ = m.Dispatch(args)
		},
	}
}

// pickWindow returns a random live window ID, or occasionally a bogus one.
func (g *opGenerator) pickWindow(m *Model) WindowID {
	if len(m.Windows) == 0 || g.rng.Intn(20) == 0 {
		return WindowID(g.rng.Intn(int(g.nextWindow + 2)))
	}
	ids := make([]WindowID, 0, len(m.Windows))
	for id := range m.Windows {
		ids = append(ids, id)
	}
	return ids[g.rng.Intn(len(ids))]
}

// pickOutput returns a random live output ID, or occasionally a bogus one.
func (g *opGenerator) pickOutput(m *Model) OutputID {
	if len(m.Outputs) == 0 || g.rng.Intn(20) == 0 {
		return OutputID(g.rng.Intn(int(g.nextOutput + 2)))
	}
	ids := make([]OutputID, 0, len(m.Outputs))
	for id := range m.Outputs {
		ids = append(ids, id)
	}
	return ids[g.rng.Intn(len(ids))]
}

// checkInvariants verifies every invariant from PLAN.md. It returns the
// first violation found.
func checkInvariants(m *Model) error {
	// 1. Every live window belongs to exactly one workspace.
	seen := make(map[WindowID]string)
	for name, ws := range m.Workspaces {
		for _, id := range ws.Windows {
			if prev, dup := seen[id]; dup {
				return fmt.Errorf("window %d is in workspaces %q and %q", id, prev, name)
			}
			seen[id] = name
			if _, ok := m.Windows[id]; !ok {
				return fmt.Errorf("workspace %q references dead window %d", name, id)
			}
		}
	}
	for id, w := range m.Windows {
		wsName, ok := seen[id]
		if !ok {
			return fmt.Errorf("window %d is in no workspace", id)
		}
		if w.Workspace != wsName {
			return fmt.Errorf("window %d thinks it is on %q but is listed on %q", id, w.Workspace, wsName)
		}
	}

	// 2. Focus index validity.
	for name, ws := range m.Workspaces {
		if len(ws.Windows) == 0 {
			if ws.Focus != -1 {
				return fmt.Errorf("empty workspace %q has focus %d", name, ws.Focus)
			}
		} else if ws.Focus < 0 || ws.Focus >= len(ws.Windows) {
			return fmt.Errorf("workspace %q focus %d out of range [0,%d)", name, ws.Focus, len(ws.Windows))
		}
	}

	// 3. Every output shows exactly one existing workspace; no two outputs
	// show the same workspace.
	shown := make(map[string]OutputID)
	for id, out := range m.Outputs {
		if out.Workspace == "" {
			return fmt.Errorf("output %d shows no workspace", id)
		}
		if _, ok := m.Workspaces[out.Workspace]; !ok {
			return fmt.Errorf("output %d shows nonexistent workspace %q", id, out.Workspace)
		}
		if prev, dup := shown[out.Workspace]; dup {
			return fmt.Errorf("outputs %d and %d both show workspace %q", prev, id, out.Workspace)
		}
		shown[out.Workspace] = id
	}

	// Focused output exists (or is 0 when there are no outputs).
	if len(m.Outputs) == 0 {
		if m.FocusedOutput != 0 {
			return fmt.Errorf("focused output %d but no outputs exist", m.FocusedOutput)
		}
	} else if _, ok := m.Outputs[m.FocusedOutput]; !ok {
		return fmt.Errorf("focused output %d does not exist", m.FocusedOutput)
	}

	// 4. Tiled windows on the same output never overlap and never escape
	// the output. 6. Arrange is deterministic.
	arr := m.Arrange()
	arr2 := m.Arrange()
	if len(arr.Order) != len(arr2.Order) {
		return fmt.Errorf("Arrange not deterministic: order lengths %d vs %d", len(arr.Order), len(arr2.Order))
	}
	for i := range arr.Order {
		if arr.Order[i] != arr2.Order[i] {
			return fmt.Errorf("Arrange not deterministic: order[%d] %d vs %d", i, arr.Order[i], arr2.Order[i])
		}
	}
	for id, p := range arr.Placements {
		if p != arr2.Placements[id] {
			return fmt.Errorf("Arrange not deterministic for window %d: %+v vs %+v", id, p, arr2.Placements[id])
		}
	}

	for _, out := range m.Outputs {
		ws := m.Workspaces[out.Workspace]
		tiled, _, _ := partition(m, ws)
		if ws.Layout == LayoutMonocle {
			continue
		}
		for i := 0; i < len(tiled); i++ {
			pi := arr.Placements[tiled[i]]
			if !pi.Visible {
				return fmt.Errorf("tiled window %d on visible workspace %q is not visible", tiled[i], ws.Name)
			}
			if !out.Rect.ContainsRect(pi.Rect) {
				return fmt.Errorf("window %d rect %v escapes output %v", tiled[i], pi.Rect, out.Rect)
			}
			for j := i + 1; j < len(tiled); j++ {
				pj := arr.Placements[tiled[j]]
				if pi.Rect.Overlaps(pj.Rect) {
					return fmt.Errorf("tiled windows %d (%v) and %d (%v) overlap on output %s",
						tiled[i], pi.Rect, tiled[j], pj.Rect, out.Name)
				}
			}
		}
	}

	// 5. Windows on hidden workspaces are not visible; all windows appear
	// in Placements.
	for id := range m.Windows {
		p, ok := arr.Placements[id]
		if !ok {
			return fmt.Errorf("window %d missing from arrangement", id)
		}
		visible := m.workspaceVisibleOn(m.Windows[id].Workspace) != 0
		if p.Visible && !visible {
			return fmt.Errorf("window %d visible but its workspace %q is hidden", id, m.Windows[id].Workspace)
		}
	}

	// Every visible window appears exactly once in the render order.
	inOrder := make(map[WindowID]int)
	for _, id := range arr.Order {
		inOrder[id]++
		if inOrder[id] > 1 {
			return fmt.Errorf("window %d appears %d times in render order", id, inOrder[id])
		}
		if !arr.Placements[id].Visible {
			return fmt.Errorf("invisible window %d is in the render order", id)
		}
	}
	for id, p := range arr.Placements {
		if p.Visible && inOrder[id] == 0 {
			return fmt.Errorf("visible window %d missing from render order", id)
		}
	}

	// Focus points at a live, visible window or nowhere.
	if arr.Focus != 0 {
		p, ok := arr.Placements[arr.Focus]
		if !ok {
			return fmt.Errorf("focus %d is not a live window", arr.Focus)
		}
		if !p.Visible {
			return fmt.Errorf("focused window %d is not visible", arr.Focus)
		}
	}

	return nil
}
