package core

import (
	"strings"
	"testing"
)

func TestSetUsageListsEveryOption(t *testing.T) {
	m := twoOutputs()
	_, err := m.Dispatch([]string{"set"})
	if err == nil {
		t.Fatal("set with no args succeeded")
	}
	// Every option handled by cmdSet must appear in the usage text so the
	// list cannot silently drift out of date.
	for _, opt := range []string{
		"main-ratio", "main-count", "main-location", "gaps", "smart-gaps",
		"border-width", "border-color-focused", "border-color-unfocused",
		"border-color-urgent", "smart-borders", "focus-follows-cursor",
		"xcursor-theme",
	} {
		if !strings.Contains(err.Error(), opt) {
			t.Errorf("set usage does not mention %q", opt)
		}
		// And every option in the list must actually be accepted (with a
		// per-option usage error rather than "unknown option").
		_, oerr := m.Dispatch([]string{"set", opt})
		if oerr != nil && strings.Contains(oerr.Error(), "unknown option") {
			t.Errorf("usage lists %q but cmdSet does not accept it", opt)
		}
	}
	_, err = m.Dispatch([]string{"set", "bogus", "1"})
	if err == nil || !strings.Contains(err.Error(), "main-ratio") {
		t.Errorf("unknown option error does not include the option list: %v", err)
	}
}
