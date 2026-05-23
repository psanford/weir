package core

import (
	"fmt"
	"path"
	"strings"
)

// RuleAction is what a window rule does to a matching window.
type RuleAction string

const (
	RuleFloat     RuleAction = "float"
	RuleNoFloat   RuleAction = "no-float"
	RuleSSD       RuleAction = "ssd"
	RuleCSD       RuleAction = "csd"
	RuleWorkspace RuleAction = "workspace"
)

// Rule matches new windows by app-id and/or title glob and applies an
// action before the window is first displayed.
type Rule struct {
	// AppID and Title are glob patterns (path.Match syntax: * ? [...]).
	// An empty pattern matches anything.
	AppID  string
	Title  string
	Action RuleAction
	// Arg is the workspace name for the workspace action.
	Arg string
}

func (r Rule) String() string {
	var b strings.Builder
	if r.AppID != "" {
		fmt.Fprintf(&b, "-app-id %q ", r.AppID)
	}
	if r.Title != "" {
		fmt.Fprintf(&b, "-title %q ", r.Title)
	}
	b.WriteString(string(r.Action))
	if r.Arg != "" {
		b.WriteString(" " + r.Arg)
	}
	return b.String()
}

// matches reports whether the rule matches a window's app-id and title.
func (r Rule) matches(appID, title string) bool {
	return globMatch(r.AppID, appID) && globMatch(r.Title, title)
}

// globMatch matches s against a path.Match pattern. An empty pattern
// matches anything; an invalid pattern matches nothing (validated at rule
// add time, so this is defensive).
func globMatch(pattern, s string) bool {
	if pattern == "" {
		return true
	}
	ok, err := path.Match(pattern, s)
	return err == nil && ok
}

// validateGlob returns an error if the pattern is not a valid path.Match
// pattern.
func validateGlob(pattern string) error {
	_, err := path.Match(pattern, "")
	return err
}

// applyRules runs every matching rule against a window, in order. Later
// matching rules override earlier ones for the same property. Rules are
// only applied while the window has never been displayed (the compositor
// has not yet reported its dimensions), so they see the window's full
// identity as it arrives but never disturb a window already in use.
func (m *Model) applyRules(w *Window) {
	if len(m.Rules) == 0 || w.ActualW != 0 || w.ActualH != 0 {
		return
	}
	for _, r := range m.Rules {
		if !r.matches(w.AppID, w.Title) {
			continue
		}
		switch r.Action {
		case RuleFloat:
			m.setFloating(w, true)
		case RuleNoFloat:
			m.setFloating(w, false)
		case RuleSSD:
			w.DecorationOverride = "ssd"
			m.markChanged()
		case RuleCSD:
			w.DecorationOverride = "csd"
			m.markChanged()
		case RuleWorkspace:
			m.MoveWindowToWorkspace(w, r.Arg)
		}
	}
}

// cmdRule implements: rule add|del|list ...
func cmdRule(m *Model, args []string) (string, error) {
	if len(args) == 0 {
		return "", cmdErrf("usage: rule add|del|list ...")
	}
	switch args[0] {
	case "list":
		var lines []string
		for _, r := range m.Rules {
			lines = append(lines, r.String())
		}
		return strings.Join(lines, "\n"), nil
	case "add", "del":
		r, err := parseRule(args[1:])
		if err != nil {
			return "", err
		}
		if args[0] == "add" {
			// Re-adding an identical rule is a no-op rather than a
			// duplicate.
			for _, existing := range m.Rules {
				if existing == r {
					return "", nil
				}
			}
			m.Rules = append(m.Rules, r)
			return "", nil
		}
		for i, existing := range m.Rules {
			if existing == r {
				m.Rules = append(m.Rules[:i], m.Rules[i+1:]...)
				return "", nil
			}
		}
		return "", cmdErrf("no such rule: %s", r)
	}
	return "", cmdErrf("usage: rule add|del|list ...")
}

// parseRule parses the [-app-id <glob>] [-title <glob>] <action> [arg]
// portion of a rule add/del command.
func parseRule(args []string) (Rule, error) {
	var r Rule
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		if len(args) < 2 {
			return r, cmdErrf("%s requires a value", args[0])
		}
		pattern := args[1]
		if _, err := path.Match(pattern, ""); err != nil {
			return r, cmdErrf("invalid glob %q: %v", pattern, err)
		}
		switch args[0] {
		case "-app-id":
			r.AppID = pattern
		case "-title":
			r.Title = pattern
		default:
			return r, cmdErrf("unknown rule option %q (want -app-id or -title)", args[0])
		}
		args = args[2:]
	}
	if r.AppID == "" && r.Title == "" {
		return r, cmdErrf("a rule needs at least one of -app-id or -title")
	}
	if len(args) == 0 {
		return r, cmdErrf("missing rule action (want float|no-float|ssd|csd|workspace <name>)")
	}
	r.Action = RuleAction(args[0])
	switch r.Action {
	case RuleFloat, RuleNoFloat, RuleSSD, RuleCSD:
		if len(args) != 1 {
			return r, cmdErrf("action %q takes no argument", args[0])
		}
	case RuleWorkspace:
		if len(args) != 2 || args[1] == "" {
			return r, cmdErrf("usage: ... workspace <name>")
		}
		r.Arg = args[1]
	default:
		return r, cmdErrf("unknown rule action %q (want float|no-float|ssd|csd|workspace)", args[0])
	}
	return r, nil
}
