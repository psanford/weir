// Command wmsim drives the weir core model with a script of events and
// commands and renders the resulting window arrangement as ASCII art. It
// exists to make the core tangible without a compositor: pipe a scenario in,
// eyeball the layout, iterate.
//
// Usage:
//
//	wmsim [script-file]   # reads stdin if no file is given
//
// Script syntax (one operation per line, # starts a comment):
//
//	output add <name> <x> <y> <w> <h>
//	output remove <name>
//	window add <label>
//	window close <label>
//	window dims <label> <w> <h>
//	window parent <child> <parent>
//	cmd <weir command...>
//	show                  # render all outputs as ASCII
//	state                 # print the JSON snapshot
//
// Example:
//
//	output add DP-1 0 0 1920 1080
//	window add a
//	window add b
//	window add c
//	cmd set main-ratio 0.6
//	show
package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/psanford/weir/core"
)

func main() {
	in := os.Stdin
	if len(os.Args) > 1 {
		f, err := os.Open(os.Args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer f.Close()
		in = f
	}

	s := newSim()
	scanner := bufio.NewScanner(in)
	lineno := 0
	interactive := in == os.Stdin && isTerminal()
	if interactive {
		fmt.Print("wmsim> ")
	}
	for scanner.Scan() {
		lineno++
		line := strings.TrimSpace(scanner.Text())
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line != "" {
			if err := s.exec(strings.Fields(line)); err != nil {
				fmt.Fprintf(os.Stderr, "line %d: %v\n", lineno, err)
				if !interactive {
					os.Exit(1)
				}
			}
		}
		if interactive {
			fmt.Print("wmsim> ")
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// sim wraps a core.Model with name<->ID mappings so scripts can refer to
// windows and outputs by human-friendly labels.
type sim struct {
	m          *core.Model
	nextWindow core.WindowID
	nextOutput core.OutputID
	windows    map[string]core.WindowID
	outputs    map[string]core.OutputID
}

func newSim() *sim {
	return &sim{
		m:       core.NewModel(),
		windows: make(map[string]core.WindowID),
		outputs: make(map[string]core.OutputID),
	}
}

func (s *sim) exec(args []string) error {
	switch args[0] {
	case "output":
		return s.execOutput(args[1:])
	case "window":
		return s.execWindow(args[1:])
	case "cmd":
		out, err := s.m.Dispatch(args[1:])
		if out != "" {
			fmt.Println(out)
		}
		return err
	case "show":
		fmt.Print(s.render())
		return nil
	case "state":
		out, err := s.m.Dispatch([]string{"get", "state"})
		fmt.Println(out)
		return err
	case "help":
		fmt.Println("output add <name> <x> <y> <w> <h> | output remove <name>")
		fmt.Println("window add <label> | window close <label> | window dims <label> <w> <h> | window parent <child> <parent>")
		fmt.Println("cmd <weir command...>   (cmd help lists commands)")
		fmt.Println("show | state")
		return nil
	default:
		return fmt.Errorf("unknown directive %q (try \"help\")", args[0])
	}
}

func (s *sim) execOutput(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: output add|remove ...")
	}
	switch args[0] {
	case "add":
		if len(args) != 6 {
			return fmt.Errorf("usage: output add <name> <x> <y> <w> <h>")
		}
		name := args[1]
		if _, exists := s.outputs[name]; exists {
			return fmt.Errorf("output %q already exists", name)
		}
		var n [4]int32
		for i, a := range args[2:6] {
			v, err := strconv.ParseInt(a, 10, 32)
			if err != nil {
				return fmt.Errorf("bad number %q", a)
			}
			n[i] = int32(v)
		}
		s.nextOutput++
		s.outputs[name] = s.nextOutput
		s.m.OutputAdded(s.nextOutput, name, core.Rect{X: n[0], Y: n[1], W: n[2], H: n[3]})
		return nil
	case "remove":
		if len(args) != 2 {
			return fmt.Errorf("usage: output remove <name>")
		}
		id, ok := s.outputs[args[1]]
		if !ok {
			return fmt.Errorf("unknown output %q", args[1])
		}
		delete(s.outputs, args[1])
		s.m.OutputRemoved(id)
		return nil
	}
	return fmt.Errorf("usage: output add|remove ...")
}

func (s *sim) execWindow(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: window add|close|dims|parent ...")
	}
	switch args[0] {
	case "add":
		if len(args) != 2 {
			return fmt.Errorf("usage: window add <label>")
		}
		if _, exists := s.windows[args[1]]; exists {
			return fmt.Errorf("window %q already exists", args[1])
		}
		s.nextWindow++
		s.windows[args[1]] = s.nextWindow
		s.m.WindowAdded(s.nextWindow)
		s.m.WindowTitle(s.nextWindow, args[1])
		return nil
	case "close":
		if len(args) != 2 {
			return fmt.Errorf("usage: window close <label>")
		}
		id, ok := s.windows[args[1]]
		if !ok {
			return fmt.Errorf("unknown window %q", args[1])
		}
		delete(s.windows, args[1])
		s.m.WindowClosed(id)
		return nil
	case "dims":
		if len(args) != 4 {
			return fmt.Errorf("usage: window dims <label> <w> <h>")
		}
		id, ok := s.windows[args[1]]
		if !ok {
			return fmt.Errorf("unknown window %q", args[1])
		}
		w, err1 := strconv.ParseInt(args[2], 10, 32)
		h, err2 := strconv.ParseInt(args[3], 10, 32)
		if err1 != nil || err2 != nil {
			return fmt.Errorf("bad dimensions")
		}
		s.m.WindowDimensions(id, int32(w), int32(h))
		return nil
	case "parent":
		if len(args) != 3 {
			return fmt.Errorf("usage: window parent <child> <parent>")
		}
		child, ok := s.windows[args[1]]
		if !ok {
			return fmt.Errorf("unknown window %q", args[1])
		}
		parent, ok := s.windows[args[2]]
		if !ok {
			return fmt.Errorf("unknown window %q", args[2])
		}
		s.m.WindowParent(child, parent)
		return nil
	}
	return fmt.Errorf("usage: window add|close|dims|parent ...")
}

// render draws each output as a grid of characters with one box per visible
// window, labeled and marked with * if focused.
func (s *sim) render() string {
	const cols, rows = 72, 18 // character cells per output

	arr := s.m.Arrange()
	labels := make(map[core.WindowID]string)
	for label, id := range s.windows {
		labels[id] = label
	}

	snap := s.m.Snapshot()
	var b strings.Builder
	if len(snap.Outputs) == 0 {
		b.WriteString("(no outputs)\n")
	}
	for _, out := range snap.Outputs {
		focusMark := ""
		if out.Focused {
			focusMark = "  [focused]"
		}
		fmt.Fprintf(&b, "─── %s  %dx%d%+d%+d  ws=%s%s\n",
			out.Name, out.Width, out.Height, out.X, out.Y, out.Workspace, focusMark)

		grid := make([][]rune, rows)
		for y := range grid {
			grid[y] = make([]rune, cols)
			for x := range grid[y] {
				grid[y][x] = ' '
			}
		}
		outRect := core.Rect{X: out.X, Y: out.Y, W: out.Width, H: out.Height}
		// Draw in render order so later (higher) windows overdraw earlier ones.
		for _, id := range arr.Order {
			p := arr.Placements[id]
			if !p.Visible || !outRect.Overlaps(p.Rect) {
				continue
			}
			drawBox(grid, outRect, p.Rect, label(labels, id), p.Focused, cols, rows)
		}
		for _, row := range grid {
			b.WriteString(strings.TrimRight(string(row), " "))
			b.WriteByte('\n')
		}
	}
	// List hidden, non-empty workspaces so they aren't forgotten.
	var hidden []string
	for _, ws := range snap.Workspaces {
		if !ws.Visible && len(ws.Windows) > 0 {
			names := make([]string, len(ws.Windows))
			for i, id := range ws.Windows {
				names[i] = label(labels, id)
			}
			hidden = append(hidden, fmt.Sprintf("%s:[%s]", ws.Name, strings.Join(names, " ")))
		}
	}
	sort.Strings(hidden)
	if len(hidden) > 0 {
		fmt.Fprintf(&b, "hidden: %s\n", strings.Join(hidden, " "))
	}
	return b.String()
}

func label(labels map[core.WindowID]string, id core.WindowID) string {
	if l, ok := labels[id]; ok {
		return l
	}
	return fmt.Sprintf("#%d", id)
}

// drawBox scales rect from output coordinates into the character grid and
// draws a bordered box with a label in the top-left corner.
func drawBox(grid [][]rune, out, rect core.Rect, name string, focused bool, cols, rows int) {
	scaleX := func(x int32) int { return int(int64(x-out.X) * int64(cols) / int64(out.W)) }
	scaleY := func(y int32) int { return int(int64(y-out.Y) * int64(rows) / int64(out.H)) }
	x0, y0 := clamp(scaleX(rect.X), 0, cols-1), clamp(scaleY(rect.Y), 0, rows-1)
	x1, y1 := clamp(scaleX(rect.X+rect.W)-1, 0, cols-1), clamp(scaleY(rect.Y+rect.H)-1, 0, rows-1)
	if x1 <= x0 {
		x1 = min(x0+1, cols-1)
	}
	if y1 <= y0 {
		y1 = min(y0+1, rows-1)
	}
	h, v := '─', '│'
	if focused {
		h, v = '═', '║'
	}
	for x := x0; x <= x1; x++ {
		grid[y0][x] = h
		grid[y1][x] = h
	}
	for y := y0; y <= y1; y++ {
		grid[y][x0] = v
		grid[y][x1] = v
	}
	corner := func(y, x int, r rune) { grid[y][x] = r }
	if focused {
		corner(y0, x0, '╔')
		corner(y0, x1, '╗')
		corner(y1, x0, '╚')
		corner(y1, x1, '╝')
	} else {
		corner(y0, x0, '┌')
		corner(y0, x1, '┐')
		corner(y1, x0, '└')
		corner(y1, x1, '┘')
	}
	// Interior fill so overlapping (floating) windows occlude what's below.
	for y := y0 + 1; y < y1; y++ {
		for x := x0 + 1; x < x1; x++ {
			grid[y][x] = ' '
		}
	}
	tag := name
	if focused {
		tag = name + "*"
	}
	for i, r := range tag {
		if x0+1+i >= x1 {
			break
		}
		grid[y0][x0+1+i] = r
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
