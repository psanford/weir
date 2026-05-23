package core

import "sort"

// Snapshot is a JSON-serializable copy of the model, returned by
// "get state" and friends. It is the scripting surface for bars and tools
// and the oracle for integration tests, so its field names are part of
// weir's public interface — change them deliberately.
type Snapshot struct {
	Mode          WorkspaceMode       `json:"mode"`
	FocusedOutput string              `json:"focused_output,omitempty"`
	FocusedWindow WindowID            `json:"focused_window,omitempty"`
	Outputs       []OutputSnapshot    `json:"outputs"`
	Workspaces    []WorkspaceSnapshot `json:"workspaces"`
	Windows       []WindowSnapshot    `json:"windows"`
}

type OutputSnapshot struct {
	Name      string `json:"name"`
	X         int32  `json:"x"`
	Y         int32  `json:"y"`
	Width     int32  `json:"width"`
	Height    int32  `json:"height"`
	Workspace string `json:"workspace"`
	Focused   bool   `json:"focused"`
}

type WorkspaceSnapshot struct {
	Name    string       `json:"name"`
	Output  string       `json:"output,omitempty"`
	Visible bool         `json:"visible"`
	Layout  LayoutName   `json:"layout"`
	Params  LayoutParams `json:"params"`
	Windows []WindowID   `json:"windows"`
	Focus   int          `json:"focus"`
}

type WindowSnapshot struct {
	ID         WindowID `json:"id"`
	AppID      string   `json:"app_id,omitempty"`
	Title      string   `json:"title,omitempty"`
	Workspace  string   `json:"workspace"`
	Floating   bool     `json:"floating,omitempty"`
	Fullscreen bool     `json:"fullscreen,omitempty"`
	X          int32    `json:"x"`
	Y          int32    `json:"y"`
	Width      int32    `json:"width"`
	Height     int32    `json:"height"`
	Visible    bool     `json:"visible"`
	Focused    bool     `json:"focused"`
}

// Snapshot builds a serializable copy of the model, including the computed
// arrangement so callers see actual geometry rather than internal state.
func (m *Model) Snapshot() Snapshot {
	arr := m.Arrange()

	s := Snapshot{
		Mode: m.Mode,
	}
	if out, ok := m.Outputs[m.FocusedOutput]; ok {
		s.FocusedOutput = out.Name
	}
	s.FocusedWindow = arr.Focus

	for _, id := range m.outputOrder {
		out := m.Outputs[id]
		s.Outputs = append(s.Outputs, OutputSnapshot{
			Name:      out.Name,
			X:         out.Rect.X,
			Y:         out.Rect.Y,
			Width:     out.Rect.W,
			Height:    out.Rect.H,
			Workspace: out.Workspace,
			Focused:   id == m.FocusedOutput,
		})
	}

	for _, name := range m.sortedWorkspaceNames() {
		ws := m.Workspaces[name]
		wss := WorkspaceSnapshot{
			Name:    name,
			Layout:  ws.Layout,
			Params:  ws.Params,
			Windows: append([]WindowID(nil), ws.Windows...),
			Focus:   ws.Focus,
		}
		if outID := m.workspaceVisibleOn(name); outID != 0 {
			wss.Output = m.Outputs[outID].Name
			wss.Visible = true
		}
		s.Workspaces = append(s.Workspaces, wss)
	}

	ids := make([]WindowID, 0, len(m.Windows))
	for id := range m.Windows {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		w := m.Windows[id]
		p := arr.Placements[id]
		s.Windows = append(s.Windows, WindowSnapshot{
			ID:         id,
			AppID:      w.AppID,
			Title:      w.Title,
			Workspace:  w.Workspace,
			Floating:   w.Floating,
			Fullscreen: w.FullscreenOn != 0,
			X:          p.Rect.X,
			Y:          p.Rect.Y,
			Width:      p.Rect.W,
			Height:     p.Rect.H,
			Visible:    p.Visible,
			Focused:    p.Focused,
		})
	}
	return s
}
