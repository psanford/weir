package core

// Placement is the complete desired state of a single window.
type Placement struct {
	// Rect is the desired geometry. For tiled windows this is the slot
	// computed by the layout; for floating windows it is the float rect;
	// for fullscreen windows it is the output rect (informational only —
	// the compositor positions fullscreen windows itself).
	Rect Rect
	// Visible is false for windows on hidden workspaces.
	Visible bool
	// Tiled is the set of edges adjacent to other tiled windows or the
	// output edge, passed to river_window_v1.set_tiled so clients can
	// adapt their decorations. Zero for floating windows.
	Tiled Edges
	// Fullscreen is the output the window should be fullscreen on, or 0.
	Fullscreen OutputID
	// Focused is true for the single window that should have keyboard
	// focus.
	Focused bool
	// Border describes the compositor-drawn border for this window.
	Border Border
}

// Edges is a bitfield of window edges, matching river_window_v1.edges.
type Edges uint32

const (
	EdgeTop    Edges = 1
	EdgeBottom Edges = 2
	EdgeLeft   Edges = 4
	EdgeRight  Edges = 8
	EdgeAll          = EdgeTop | EdgeBottom | EdgeLeft | EdgeRight
)

// Border describes a compositor-drawn border.
type Border struct {
	Width int32
	Color uint32 // 0xRRGGBBAA
}

// Arrangement is the complete desired state of the world: where every window
// goes, in what stacking order, and which one has focus. The bridge
// translates an Arrangement into protocol requests; tests assert on it
// directly.
type Arrangement struct {
	// Placements has an entry for every live window.
	Placements map[WindowID]Placement
	// Order is the desired render order, bottom to top. Only visible
	// windows appear in Order.
	Order []WindowID
	// Focus is the window that should have keyboard focus, or 0 to clear
	// focus.
	Focus WindowID
}

// Arrange computes the desired state of every window from the current model.
// It is deterministic and has no side effects.
func (m *Model) Arrange() Arrangement {
	arr := Arrangement{
		Placements: make(map[WindowID]Placement, len(m.Windows)),
	}

	// Default: every window is invisible until proven otherwise. Windows on
	// hidden workspaces keep their last geometry but are not shown.
	for id := range m.Windows {
		arr.Placements[id] = Placement{Visible: false}
	}

	// Stacking order is built up in layers, bottom to top:
	//   tiled windows of every visible workspace,
	//   then floating windows,
	//   then fullscreen windows.
	var tiledOrder, floatOrder, fullscreenOrder []WindowID

	for _, outID := range m.outputOrder {
		out := m.Outputs[outID]
		ws, ok := m.Workspaces[out.Workspace]
		if !ok {
			continue
		}

		tiled, floating, fullscreen := partition(m, ws)

		// Lay out the tiled windows.
		rects := ComputeLayout(ws.Layout, out.Rect, len(tiled), ws.Params)
		smartBorderless := m.Borders.SmartBorders && len(tiled) == 1 && len(fullscreen) == 0
		for i, id := range tiled {
			p := arr.Placements[id]
			p.Visible = true
			p.Rect = rects[i]
			p.Tiled = tiledEdges(rects[i], rects, out.Rect)
			if !smartBorderless {
				p.Border = m.borderFor(ws, i)
			}
			arr.Placements[id] = p
			tiledOrder = append(tiledOrder, id)
		}

		for _, id := range floating {
			w := m.Windows[id]
			p := arr.Placements[id]
			p.Visible = true
			p.Rect = w.FloatRect
			p.Border = m.borderFor(ws, indexOf(ws.Windows, id))
			arr.Placements[id] = p
			floatOrder = append(floatOrder, id)
		}

		for _, id := range fullscreen {
			w := m.Windows[id]
			p := arr.Placements[id]
			p.Visible = true
			p.Rect = out.Rect
			p.Fullscreen = w.FullscreenOn
			arr.Placements[id] = p
			fullscreenOrder = append(fullscreenOrder, id)
		}
	}

	arr.Order = append(arr.Order, tiledOrder...)
	arr.Order = append(arr.Order, floatOrder...)
	arr.Order = append(arr.Order, fullscreenOrder...)

	if fw := m.FocusedWindow(); fw != nil {
		arr.Focus = fw.ID
		p := arr.Placements[fw.ID]
		p.Focused = true
		arr.Placements[fw.ID] = p
	}

	return arr
}

// partition splits a workspace's windows into tiled, floating, and
// fullscreen groups, preserving stack order within each group.
func partition(m *Model, ws *Workspace) (tiled, floating, fullscreen []WindowID) {
	for _, id := range ws.Windows {
		w := m.Windows[id]
		if w == nil {
			continue
		}
		switch {
		case w.FullscreenOn != 0:
			fullscreen = append(fullscreen, id)
		case w.Floating:
			floating = append(floating, id)
		default:
			tiled = append(tiled, id)
		}
	}
	return tiled, floating, fullscreen
}

// borderFor returns the border for the window at stack index i of ws.
func (m *Model) borderFor(ws *Workspace, i int) Border {
	color := m.Borders.UnfocusedColor
	if i >= 0 && i == ws.Focus && m.workspaceVisibleOn(ws.Name) == m.FocusedOutput {
		color = m.Borders.FocusedColor
	}
	return Border{Width: m.Borders.Width, Color: color}
}

// tiledEdges computes which edges of rect are adjacent to either another
// tiled window or the output boundary. River forwards this to clients so
// they can square off their corners and drop shadows on shared edges.
func tiledEdges(rect Rect, all []Rect, output Rect) Edges {
	var e Edges
	if rect.Y == output.Y || anyAdjacent(all, rect, EdgeTop) {
		e |= EdgeTop
	}
	if rect.Y+rect.H == output.Y+output.H || anyAdjacent(all, rect, EdgeBottom) {
		e |= EdgeBottom
	}
	if rect.X == output.X || anyAdjacent(all, rect, EdgeLeft) {
		e |= EdgeLeft
	}
	if rect.X+rect.W == output.X+output.W || anyAdjacent(all, rect, EdgeRight) {
		e |= EdgeRight
	}
	return e
}

// anyAdjacent reports whether any rect in all (other than r itself) touches
// r on the given edge.
func anyAdjacent(all []Rect, r Rect, edge Edges) bool {
	for _, o := range all {
		if o == r {
			continue
		}
		switch edge {
		case EdgeTop:
			if o.Y+o.H <= r.Y && spansOverlap(o.X, o.W, r.X, r.W) {
				return true
			}
		case EdgeBottom:
			if o.Y >= r.Y+r.H && spansOverlap(o.X, o.W, r.X, r.W) {
				return true
			}
		case EdgeLeft:
			if o.X+o.W <= r.X && spansOverlap(o.Y, o.H, r.Y, r.H) {
				return true
			}
		case EdgeRight:
			if o.X >= r.X+r.W && spansOverlap(o.Y, o.H, r.Y, r.H) {
				return true
			}
		}
	}
	return false
}

func spansOverlap(a, alen, b, blen int32) bool {
	return a < b+blen && b < a+alen
}

func indexOf(ids []WindowID, id WindowID) int {
	for i, x := range ids {
		if x == id {
			return i
		}
	}
	return -1
}
