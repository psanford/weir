# waybar integration

river 0.4 removed `river-status-unstable-v1`, so waybar's built-in
`river/tags` and `river/window` modules do not work with river 0.4 or weir.
Feed `custom` modules from weir's control socket instead.

Install `weir-workspaces` somewhere on your `PATH` (it needs `weirctl` and
`jq`).

> **PATH gotcha:** waybar inherits the environment of the river init script,
> which is a plain `/bin/sh` that never sources your shell rc files. If
> `weirctl` lives in `~/go/bin`, add `export PATH="$HOME/go/bin:$PATH"` to
> the init script before `waybar &`. The module renders
> "weir: weirctl not found in PATH" in the bar if this is the problem; to
> debug further, run `weir-workspaces` by hand in a terminal inside the
> session and see what it prints.

## Clickable per-workspace buttons (recommended)

Waybar can only attach one click action to a custom module, so per-workspace
buttons need one module instance per workspace. Each instance shows that
workspace's name, carries a CSS class for its state
(`focused`/`visible`/`occupied`/`empty`), and jumps to the workspace on
click.

`~/.config/waybar/config`:

```json
"modules-left": [
    "custom/ws1", "custom/ws2", "custom/ws3", "custom/ws4", "custom/ws5",
    "custom/ws6", "custom/ws7", "custom/ws8", "custom/ws9"
],
"custom/ws1": { "exec": "weir-workspaces 1", "on-click": "weirctl view 1", "return-type": "json", "format": "{}", "tooltip": true },
"custom/ws2": { "exec": "weir-workspaces 2", "on-click": "weirctl view 2", "return-type": "json", "format": "{}", "tooltip": true },
"custom/ws3": { "exec": "weir-workspaces 3", "on-click": "weirctl view 3", "return-type": "json", "format": "{}", "tooltip": true },
"custom/ws4": { "exec": "weir-workspaces 4", "on-click": "weirctl view 4", "return-type": "json", "format": "{}", "tooltip": true },
"custom/ws5": { "exec": "weir-workspaces 5", "on-click": "weirctl view 5", "return-type": "json", "format": "{}", "tooltip": true },
"custom/ws6": { "exec": "weir-workspaces 6", "on-click": "weirctl view 6", "return-type": "json", "format": "{}", "tooltip": true },
"custom/ws7": { "exec": "weir-workspaces 7", "on-click": "weirctl view 7", "return-type": "json", "format": "{}", "tooltip": true },
"custom/ws8": { "exec": "weir-workspaces 8", "on-click": "weirctl view 8", "return-type": "json", "format": "{}", "tooltip": true },
"custom/ws9": { "exec": "weir-workspaces 9", "on-click": "weirctl view 9", "return-type": "json", "format": "{}", "tooltip": true },
```

`~/.config/waybar/style.css`:

```css
[id^="custom-ws"] {
    padding: 0 6px;
    margin: 0 1px;
    border-bottom: 2px solid transparent;
    color: #586e75;
}
[id^="custom-ws"].occupied { color: #93a1a1; }
[id^="custom-ws"].visible  { color: #93a1a1; border-bottom-color: #586e75; }
[id^="custom-ws"].focused  { color: #fdf6e3; border-bottom-color: #93a1a1; }
/* Hide empty workspaces entirely, if you prefer: */
/* [id^="custom-ws"].empty { font-size: 0; padding: 0; margin: 0; } */
```

(If your waybar's CSS does not support `[id^=...]` attribute selectors, list
the modules explicitly: `#custom-ws1, #custom-ws2, ... { ... }`.)

Scroll-to-switch works by adding `"on-scroll-up": "weirctl view prev"` and
`"on-scroll-down": "weirctl view next"` to any (or every) instance.

## Single combined module

One module, one config block, but the whole label is a single click target.
Shows every workspace: the focused one bracketed and bold, occupied ones
marked with a dot.

```json
"modules-left": ["custom/weir"],
"custom/weir": {
    "exec": "weir-workspaces",
    "return-type": "json",
    "escape": false,
    "tooltip": true,
    "on-click": "weirctl view next",
    "on-scroll-up": "weirctl view prev",
    "on-scroll-down": "weirctl view next"
}
```

## Anything else

The same `weirctl subscribe` stream carries the full state snapshot — the
focused window title, layouts, per-output workspaces — so a window-title
module or anything else is a different `jq` expression away. `weirctl get
state | jq .` shows everything available. Each subscriber gets the current
state immediately on connect and only the latest state if it falls behind.
