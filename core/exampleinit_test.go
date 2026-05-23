package core

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestExampleInitMentionsEveryCommand enforces that example/init stays the
// complete reference configuration: every command in the command table and
// every option accepted by "set" must appear in it (a commented mention
// counts). Adding a command or option without documenting it there fails
// this test.
func TestExampleInitMentionsEveryCommand(t *testing.T) {
	data, err := os.ReadFile("../example/init")
	if err != nil {
		t.Fatalf("reading example/init: %v", err)
	}
	init := string(data)

	for i := range commands {
		name := commands[i].name
		// "weirctl <name>" anywhere in the file, including inside
		// comments and after a "bind <chord>" prefix.
		re := regexp.MustCompile(`(weirctl |bind [^ ]+ +|bind-pointer [^ ]+ +)` + regexp.QuoteMeta(name) + `\b`)
		if !re.MatchString(init) {
			t.Errorf("example/init does not mention the %q command", name)
		}
	}

	// Every "set" option from the usage text must appear as "set <option>".
	for _, line := range strings.Split(setUsage, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasSuffix(line, ":") || strings.HasPrefix(line, "usage:") {
			continue
		}
		opt := strings.Fields(line)[0]
		if !strings.Contains(init, "set "+opt) {
			t.Errorf("example/init does not mention \"set %s\"", opt)
		}
	}

	// weirctl-side commands that are not in the core command table but are
	// part of the documented surface.
	for _, extra := range []string{"wait-for-socket", "subscribe"} {
		if !strings.Contains(init, extra) {
			t.Errorf("example/init does not mention %q", extra)
		}
	}
}
