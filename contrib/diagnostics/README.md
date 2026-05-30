# Diagnostics

Ad-hoc diagnostic scripts for debugging weir/river/desktop-stack issues on
real hardware. These live on the `diagnostics` branch rather than `main`
because they are investigation tooling, not part of weir.

| Script | When to run it |
| --- | --- |
| `diagnose-dark-screen` | The screen stays black after DPMS/wlopm re-enable. Run from a TTY or SSH while it is stuck. |
| `diagnose-missing-notifications` | Mako notifications stop appearing. Run inside the session while they are broken. |

Each script prints a verdict that points at the responsible layer
(kernel / river / weir / the client) and what evidence to capture next.
