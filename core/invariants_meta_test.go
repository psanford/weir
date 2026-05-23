package core

import (
	"strings"
	"testing"
)

// TestInvariantCheckerDetectsCorruption deliberately corrupts the model in
// each of the ways the invariants are supposed to forbid and asserts that
// checkInvariants notices. Without this, a bug in checkInvariants that made
// it always return nil would silently neuter the entire property suite.
func TestInvariantCheckerDetectsCorruption(t *testing.T) {
	cases := []struct {
		name    string
		corrupt func(m *Model)
		wantMsg string
	}{
		{
			name: "window in two workspaces",
			corrupt: func(m *Model) {
				ws := m.ensureWorkspace("evil")
				ws.Windows = append(ws.Windows, 10)
				ws.Focus = 0
			},
			wantMsg: "is in workspaces",
		},
		{
			name: "window in no workspace",
			corrupt: func(m *Model) {
				ws := m.Workspaces["1"]
				ws.Windows = nil
				ws.Focus = -1
			},
			wantMsg: "is in no workspace",
		},
		{
			name: "workspace references dead window",
			corrupt: func(m *Model) {
				ws := m.Workspaces["1"]
				ws.Windows = append(ws.Windows, 999)
			},
			wantMsg: "dead window",
		},
		{
			name: "focus out of range",
			corrupt: func(m *Model) {
				m.Workspaces["1"].Focus = 5
			},
			wantMsg: "out of range",
		},
		{
			name: "empty workspace with focus",
			corrupt: func(m *Model) {
				m.ensureWorkspace("empty").Focus = 0
			},
			wantMsg: "has focus",
		},
		{
			name: "two outputs showing the same workspace",
			corrupt: func(m *Model) {
				m.Outputs[2].Workspace = "1"
			},
			wantMsg: "both show workspace",
		},
		{
			name: "output showing nonexistent workspace",
			corrupt: func(m *Model) {
				m.Outputs[1].Workspace = "ghost"
			},
			wantMsg: "nonexistent workspace",
		},
		{
			name: "focused output does not exist",
			corrupt: func(m *Model) {
				m.FocusedOutput = 42
			},
			wantMsg: "does not exist",
		},
		{
			name: "window mislabeled with wrong workspace",
			corrupt: func(m *Model) {
				m.Windows[10].Workspace = "2"
			},
			wantMsg: "thinks it is on",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := twoOutputs()
			m.WindowAdded(10)
			if err := checkInvariants(m); err != nil {
				t.Fatalf("invariants violated before corruption: %v", err)
			}
			tc.corrupt(m)
			err := checkInvariants(m)
			if err == nil {
				t.Fatalf("checkInvariants did not detect: %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error %q does not contain %q", err, tc.wantMsg)
			}
		})
	}
}
