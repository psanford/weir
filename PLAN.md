# weir — design & build plan

weir is a window manager for [river](https://codeberg.org/river/river) (the
0.4+ non-monolithic compositor), written in Go. It implements the
`river-window-management-v1` protocol as a separate process.

A weir is a small dam that controls a river's flow.

## Goals

- **xmonad-style dynamic tiling** — master/stack with main ratio, main count,
  main location, gaps, smart gaps, and a monocle mode (the rivercarro feature
  set), plus floating and fullscreen windows.
- **Fully programmatic** — no config file. Everything is a runtime command
  over a unix socket, with a `weirctl` CLI wrapper. The init script is just a
  list of `weirctl` calls, river-classic style. Anything configurable is
  queryable.
- **First-class multi-output support** — directional focus/send between
  outputs, graceful output removal (windows are never lost), and two
  workspace modes selectable at runtime:
  - `independent` (xmonad/dwm): workspaces are global, each output views one.
  - `locked` (gnome/kde): switching desktops retargets every output at once.
- **Rock solid** — invariants enforced by property-based tests; the WM can
  crash and be restarted without losing the session (river re-sends all
  state to a new window manager client).
- **Easy to test** — the overwhelming majority of logic is testable with
  `go test` alone, no compositor, no display. Full-stack integration tests
  run against a real headless river in CI.
- **Easy to extend** — layouts are pure functions behind a small interface;
  commands are entries in a table; the protocol layer never makes decisions.

## Non-goals (for now)

- Drawing anything. No bar, no wallpaper, no client-side titlebars. River
  draws borders compositor-side (`river_window_v1.set_borders`); bars and
  wallpaper are external programs fed by weir's IPC event stream.
- Xwayland-specific behavior beyond what the protocol abstracts away.
- Animations.

## Fixed decisions

| Decision | Choice | Why |
| --- | --- | --- |
| Language | Go | requested |
| Wayland client | pure Go, no cgo | static binaries, simple cross-compile and CI, race detector works; codegen from river's protocol XML |
| Rendering | none in v1 | borders are compositor-side; everything else is external |
| IPC | unix socket in `$XDG_RUNTIME_DIR`, newline-delimited JSON | proven (i3/sway), scriptable from anything |
| Config | none — runtime commands only | requested; matches river-classic |
| Module path | `github.com/psanford/weir` | change with one sed if hosted elsewhere |

## Architecture

```
┌────────────────────────────────────────────────────────┐
│ core/        pure Go, zero Wayland imports             │
│   model:     outputs, workspaces, windows, focus       │
│   layout:    tile (master/stack), monocle              │
│   commands:  the single dispatch point for ALL actions │
└──────────────▲─────────────────────────┬───────────────┘
        events │                         │ Arrangement
┌──────────────┴─────────────────────────▼───────────────┐
│ bridge/      river protocol adapter (Wayland client)   │
│   owns the manage/render sequence state machine        │
│   diffs desired Arrangement against last-sent state    │
└──────────────▲─────────────────────────┬───────────────┘
               │      Wayland socket     │
┌──────────────┴─────────────────────────▼───────────────┐
│ river        (real compositor: DRM, nested, headless)  │
└────────────────────────────────────────────────────────┘

┌──────────┐   unix socket, JSON    ┌──────────────┐
│ weirctl  │ ─────────────────────▶ │ ipc/ server  │──▶ core commands
└──────────┘  command/query/events  └──────────────┘
```

Rules that keep this honest:

1. `core/` never imports anything Wayland-related. It is a deterministic
   state machine: events in, an `Arrangement` (the complete desired state of
   every window) out.
2. The bridge never makes policy decisions. It translates protocol events
   into core events and the core's `Arrangement` into protocol requests.
3. Keybindings, pointer bindings, and the IPC socket all dispatch through
   the same command table. One code path per action.

### Why this is testable

The river protocol drives the WM through an explicit transaction loop
(`manage_start` → mutate → `manage_finish` → `render_start` → position →
`render_finish`, all double-buffered). The WM is therefore a pure function of
its inputs. Keeping that function free of I/O means:

- **Tier 1 — unit + property tests of `core/`** (no compositor, no display).
  Synthetic events in, assertions on geometry/focus out. Property tests
  enforce the invariants below over random operation sequences.
- **Tier 2 — protocol-sequence tests of `bridge/`** against a scripted fake
  peer. The manage/render ordering rules are protocol errors if violated, so
  the sequencing state machine gets its own tests.
- **Tier 3 — integration tests against real river, headless.**
  `WLR_BACKENDS=headless WLR_RENDERER=pixman WLR_LIBINPUT_NO_DEVICES=1
  river -c '...'` runs the full stack with no GPU and no display. The test
  harness spawns real clients and asserts via `weirctl get-state` (the
  introspection API doubles as the test oracle). Headless wlroots can add and
  remove virtual outputs at runtime, which is how output hotplug is tested.

Interactive development: river auto-detects a parent Wayland/X11 session and
runs nested in a window, so `river -c weir` from a terminal gives a sandboxed
compositor without touching the real session.

### Invariants (enforced by property tests)

1. Every live window belongs to exactly one workspace.
2. Every workspace's focus index is in range, or -1 iff the workspace is
   empty.
3. Every output shows exactly one existing workspace; no two outputs show the
   same workspace.
4. Tiled windows on the same output never overlap and never extend outside
   the output.
5. Removing an output never removes a window; orphaned workspaces are adopted
   by a surviving output (or become hidden if no outputs remain).
6. `Arrange()` is deterministic and idempotent.

## Data model (core/)

- **Window** — protocol identity, app_id/title/parent, the workspace it
  belongs to, floating/fullscreen flags, float geometry, last known actual
  dimensions.
- **Workspace** — a named, ordered stack of windows (index 0 = first main
  window), a focused index, a layout name, and layout params. Created on
  first reference, never destroyed.
- **Output** — name, geometry, and the workspace it currently shows.
- **Workspace modes** — in `independent` mode the user-facing workspace name
  is the internal name. In `locked` mode the user-facing name is a desktop
  name that expands to one internal workspace per output (`3` → `3@DP-1`,
  `3@HDMI-A-1`); `view 3` switches every output in a single atomic manage
  sequence. Only command-name resolution knows about modes; the model
  doesn't.
- **Floating** — dialogs (windows with a parent) float by default; floating
  windows render above tiled ones and keep their own geometry.
- **Fullscreen** — tracked per window, positioned by the compositor; the
  window keeps its slot in the workspace stack.

## Command surface (initial set)

```
focus next|prev|main                  swap next|prev|main
close                                 zoom            # promote to main
view <ws>                             send <ws>
pull <ws>                             # greedyView: bring ws here, swapping
focus-output <dir|name>               send-to-output <dir|name>
set-layout tile|monocle
set main-ratio <r|+d|-d>              set main-count <n|+d|-d>
set main-location left|right|top|bottom
set gaps <inner> <outer>              set smart-gaps on|off
toggle-float                          toggle-fullscreen
workspace-mode independent|locked
get state | outputs | windows         # JSON
subscribe                             # event stream
bind <mods+key> <command...>          unbind <mods+key>
spawn <command...>
exit
```

`<dir>` is `next|prev|left|right|up|down`; directional resolution uses the
output positions river reports.

## Milestones

Each milestone ends in something runnable and tested.

- [x] **M1 — `core/`**: geometry, layouts, model, arrangement, command
      dispatcher; unit tests, property tests; `cmd/wmsim` ASCII simulator
      that replays a script of events/commands and renders the layout.
- [x] **M2 — wire + codegen**: pure-Go Wayland client connection
      (`wire/`) and a generator that produces typed bindings from river's
      six protocol XML files (`internal/gen/`).
- [x] **M3 — `bridge/`**: connect to river, implement the manage/render
      state machine, diff-and-send the core's Arrangement. Verified against
      a fake river compositor that enforces the protocol's sequencing rules;
      not yet run against a real river (no zig/wlroots in the dev container
      — see M5).
- [x] **M4 — IPC + `weirctl`**: socket server, command/query/subscribe,
      CLI. Everything drivable and inspectable from the shell.
- [x] **M5 — headless integration harness**: run river headless in CI,
      spawn test clients, assert via `weirctl`. Output hotplug tests using
      headless virtual outputs (disabled and re-enabled with wlr-randr).
      `.github/workflows/ci.yml` runs the unit suite and all three
      compositor-backed test scripts.
- [x] **M6 — input**: keyboard bindings (`river-xkb-bindings-v1`), pointer
      bindings + interactive move/resize (`op_start_pointer`), focus follows
      interaction policy, spawn. Key presses are integration-tested end to
      end by injecting them through a virtual keyboard (wtype).
- [x] **M7 — multi-output polish**: locked workspace mode, directional
      navigation, output remove/re-add restoration (an output that comes
      back gets the workspace it was showing when it left, keyed by output
      name so it survives the synthetic-name-then-rename dance).

## Known issues and gaps

Updated as real-hardware testing surfaces them. Roughly ordered by how much
they block daily use.

### Missing features that block a normal desktop setup

- **Status bar integration.** river 0.4 removed `river-status-unstable-v1`,
  so waybar's built-in river module does not work at all. The replacement is
  a custom waybar module (or any bar) fed by `weirctl subscribe` /
  `weirctl get state`; weir should ship a small example.
- **Window rules.** No `rule add -app-id mpv float` equivalent. Float, CSD,
  and workspace-assignment rules keyed on app-id/title.
- **Per-keyboard layouts.** Needed: at least one user runs two keyboards
  with different layouts simultaneously, which is exactly what
  `river-xkb-config-v1` exists for (each physical keyboard gets its own
  `river_xkb_keyboard_v1` with a per-device `set_keymap`).

  Interim workaround: `XKB_DEFAULT_LAYOUT` / `XKB_DEFAULT_VARIANT` /
  `XKB_DEFAULT_OPTIONS` in the environment before river starts — global to
  all keyboards and fixed at launch.

  Design notes for the implementation:
  - `create_keymap` takes an fd holding a *compiled* XKB keymap, not
    rules/model/layout/variant/options names. Compiling RMLVO is
    libxkbcommon's job; to stay cgo-free, shell out to
    `xkbcli compile-keymap --layout ... --options ...` (ships with
    libxkbcommon), write its output to a memfd, pass that fd. Degrade with
    a clear error if `xkbcli` is missing.
  - Wait for the keymap object's `success`/`failure` event before using it
    with `set_keymap`; surface `failure`'s error message to the user.
  - Per-device targeting requires binding `river_input_manager_v1` to learn
    device names — the same plumbing needed for libinput settings (below).
  - Command surface:
    `keyboard-layout [-rules R] [-model M] [-variant V] [-options O]
    [-device <name>] <layout>` (no `-device` = all current and future
    keyboards), `list-inputs` to discover device names. Remember the
    configured keymap and apply it to keyboards plugged in later.
  - Follow-ups enabled by the same protocol: runtime layout switching
    within a multi-layout keymap (`set_layout_by_name`, bindable to a key)
    and capslock/numlock control.
- **Input device configuration.** Keyboard repeat rate, natural scrolling,
  tap-to-click, per-device settings (`river-input-management-v1` /
  `river-libinput-config-v1` are generated but unused). Shares the
  input-manager plumbing with per-keyboard layouts above.

### Missing commands with no workaround

- Runtime configuration of the default workspace set (fixed at 1-9).

### Behavioral rough edges

- focus-follows-cursor also raises the hovered window within its group
  (focus and stacking are coupled); hovering across overlapping floating
  windows shuffles their z-order.

- A window that takes a smaller size than its tile slot (terminal cell
  snapping) sits at the slot's top-left with a gap at the bottom/right
  rather than being centered in the slot.
- Interactive resize always tracks the bottom-right corner regardless of
  which edge a CSD titlebar drag requested.
- xdg-activation requests (an app asking to be focused/marked urgent) are
  ignored; the urgent border color is defined but never used.
- Maximize, minimize, and window-menu requests from clients are ignored
  (capabilities advertise fullscreen only, so compliant clients hide those
  buttons).

### Untested territory

- Xwayland windows (override-redirect popups, position hints).
- Crash-restart recovery (designed: river re-sends all state to a new WM
  client; never exercised outside of theory).
- Session lock interaction (weir logs the events and otherwise ignores them).
- Real multi-monitor hardware (mixed scale factors in particular).

### By design

- dwm/river-classic-style tags (viewing multiple workspaces at once,
  windows on multiple workspaces). weir uses xmonad workspaces.
- Binding modes (`declare-mode passthrough`).
- Multi-seat.
- Drawing anything (bars, wallpaper, titlebars) inside weir itself.

## Reference material

- Protocol files: `~/proj/river/protocol/*.xml` (especially
  `river-window-management-v1.xml` — read its interface description first;
  the manage/render sequence rules are the heart of the protocol).
- `tinyrwm` (https://codeberg.org/river/tinyrwm) — minimal reference WM by
  river's author.
- `croffle` (https://codeberg.org/vyivel/croffle) — existing Go WM for
  river; useful as a second opinion on Go Wayland plumbing.
- xmonad's `StackSet` module — the model weir's workspace handling is
  adapted from.
