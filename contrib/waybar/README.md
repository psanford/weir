# waybar integration

river 0.4 removed `river-status-unstable-v1`, so waybar's built-in
`river/tags` and `river/window` modules do not work with river 0.4 or weir.
Feed a `custom` module from weir's control socket instead.

Install `weir-workspaces` somewhere on your `PATH` (it needs `weirctl` and
`jq`), then add to `~/.config/waybar/config`:

> **PATH gotcha:** waybar inherits the environment of the river init script,
> which is a plain `/bin/sh` that never sources your shell rc files. If
> `weirctl` lives in `~/go/bin`, add `export PATH="$HOME/go/bin:$PATH"` to
> the init script before `waybar &`. The module renders
> "weir: weirctl not found in PATH" in the bar if this is the problem; to
> debug further, run `weir-workspaces` by hand in a terminal inside the
> session and see what it prints.

```json
"modules-left": ["custom/weir"],
"custom/weir": {
    "exec": "weir-workspaces",
    "return-type": "json",
    "escape": false,
    "tooltip": true
}
```

The module renders the focused workspace in bold brackets and lists every
visible or occupied workspace. It updates on every weir state change (the
subscription delivers the current state immediately on connect, and only the
latest state if waybar falls behind) and reconnects automatically if weir
restarts.

The same `weirctl subscribe` stream carries the full state snapshot — the
focused window title, layouts, per-output workspaces — so a window-title
module or anything else is a different `jq` expression away. `weirctl get
state | jq .` shows everything available.
