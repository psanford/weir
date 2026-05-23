# weir

A window manager for [river](https://codeberg.org/river/river) 0.4+, written
in Go. xmonad-style dynamic tiling, fully programmatic configuration over a
unix socket, first-class multi-output support.

A weir is a small dam that controls a river's flow.

**Status: early development, but functional.** Tiling, workspaces,
multi-output, key/pointer bindings, and the control socket all work against
river 0.4.5. See [PLAN.md](PLAN.md) for the design and roadmap, and
[example/init](example/init) for a complete xmonad-flavored configuration.

```sh
go install github.com/psanford/weir/cmd/weir@latest
go install github.com/psanford/weir/cmd/weirctl@latest
cp example/init ~/.config/river/init && chmod +x ~/.config/river/init
river
```

## Layout

| Path | What |
| --- | --- |
| `core/` | The window-management state machine: model, layouts, commands. Pure Go, no Wayland imports, fully unit-tested. |
| `bridge/` | The river protocol adapter: owns the manage/render sequence loop and translates between protocol events and the core model. Tested against a fake compositor that enforces the protocol's sequencing rules. |
| `ipc/` | The control socket: newline-delimited JSON over a unix socket. Commands, queries, and a state-change subscription stream for bars. |
| `cmd/weir/` | The window manager binary. Start it from river's init script. |
| `cmd/weirctl/` | The CLI: `weirctl focus next`, `weirctl get state`, `weirctl subscribe`, `weirctl help`. |
| `wire/` | Pure-Go Wayland client wire protocol: connection, marshalling, fd passing, object lifetime, and the hand-written `wl_display`/`wl_registry`/`wl_callback` bootstrap. No cgo. |
| `wire/wiretest/` | A fake compositor speaking the raw wire format over a socketpair, for testing protocol code without river. |
| `protocol/` | Vendored protocol XML (wayland core + river's six extensions). |
| `protocols/wl/`, `protocols/river/` | Generated typed bindings. Regenerate with `go generate ./...`. |
| `internal/gen/` | The protocol code generator. |
| `cmd/wmsim/` | ASCII simulator: replay a script of events and commands against the core and render the resulting layout. |
| `example/` | wmsim scenario scripts. |

## Developing

```sh
go test ./...          # unit + property + protocol tests, no compositor needed
go run ./cmd/wmsim example/two-outputs.txt
go run ./cmd/wmsim     # interactive REPL ("help" for syntax)
```

The property tests in `core/invariants_test.go` drive the model with tens of
thousands of random operations and check the structural invariants from
PLAN.md after every step. If you change the model, that suite is the first
thing to trust. The bridge tests in `bridge/` run against a fake compositor
that fails the test if a request is ever sent in an illegal protocol phase.

### Against a real river

`scripts/integration-test.sh` runs weir inside a real headless river
(wlroots headless backend + pixman renderer — no GPU, no display, no seat),
opens terminals, drives weir with `weirctl`, and asserts on the JSON state
it reports. `scripts/smoke-test.sh` is a faster log-based check and
`scripts/screenshot-test.sh` captures a PNG of the tiled layout via grim.

```sh
eval "$(scripts/fetch-river.sh)"   # sets $RIVER and $FOOT from the nix cache
scripts/smoke-test.sh
```

Or run it nested in your current desktop session to interact with it:

```sh
go build ./cmd/weir && river -c ./weir
```

## License

MIT. The protocol definitions vendored under `protocol/` carry their own
permissive licenses in their embedded `<copyright>` blocks (wayland.xml from
the Wayland project, river-*.xml from river).
