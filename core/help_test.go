package core

import (
	"strings"
	"testing"
)

// TestHelpListsEveryCommandInAKnownGroup ensures no command silently
// disappears from the help output because of a missing or misspelled group.
func TestHelpListsEveryCommandInAKnownGroup(t *testing.T) {
	known := make(map[string]bool, len(helpGroups))
	for _, g := range helpGroups {
		known[g] = true
	}
	out := run(t, NewModel(), "help")
	for i := range commands {
		if !known[commands[i].group] {
			t.Errorf("command %q has unknown group %q", commands[i].name, commands[i].group)
		}
		if !strings.Contains(out, commands[i].usage) && !strings.Contains(out, commands[i].name) {
			t.Errorf("help output does not mention %q", commands[i].name)
		}
	}
	for _, g := range helpGroups {
		if !strings.Contains(out, g+":") {
			t.Errorf("help output missing the %q section", g)
		}
	}
}
