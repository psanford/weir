# weir

A window manager for [river](https://codeberg.org/river/river) 0.4+, written
in Go. xmonad-style dynamic tiling, fully programmatic configuration over a
unix socket, first-class multi-output support.

A weir is a small dam that controls a river's flow.

**Status: early development.** The pure window-management core (model,
layouts, commands) is implemented and tested; the Wayland protocol layer is
not yet written. See [PLAN.md](PLAN.md) for the design and roadmap.

## Layout

| Path | What |
| --- | --- |
| `core/` | The window-management state machine: model, layouts, commands. Pure Go, no Wayland imports, fully unit-tested. |
| `cmd/wmsim/` | ASCII simulator: replay a script of events and commands against the core and render the resulting layout. |
| `example/` | wmsim scenario scripts. |

## Developing

```sh
go test ./...          # unit + property tests, no compositor needed
go run ./cmd/wmsim example/two-outputs.txt
go run ./cmd/wmsim     # interactive REPL ("help" for syntax)
```

The property tests in `core/invariants_test.go` drive the model with tens of
thousands of random operations and check the structural invariants from
PLAN.md after every step. If you change the model, that suite is the first
thing to trust.
