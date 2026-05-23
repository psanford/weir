#!/bin/sh
# Multi-output integration test: run weir in a headless river with two
# virtual outputs and verify workspace assignment, cross-output movement,
# locked workspace mode, and output hotplug (via wlr-randr disabling and
# re-enabling an output).
#
# Requires river >= 0.4, foot, jq, and optionally wlr-randr for the hotplug
# section. Set $RIVER, $FOOT, and $WLR_RANDR to override PATH lookup.
set -eu

RIVER="${RIVER:-river}"
FOOT="${FOOT:-foot}"

dir="$(mktemp -d /tmp/weir-itest2.XXXXXX)"
trap 'rm -rf "$dir"' EXIT
mkdir -p -m 0700 "$dir/run"

repo="$(cd "$(dirname "$0")/.." && pwd)"
go build -o "$dir/weir" "$repo/cmd/weir"
go build -o "$dir/weirctl" "$repo/cmd/weirctl"

cat > "$dir/test" <<'TESTEOF'
#!/bin/sh
set -u
verdict="$WEIR_TEST_DIR/verdict"
ctl="$WEIR_TEST_DIR/weirctl"

ok() { echo "ok: $1" >>"$verdict"; }
fail() { echo "FAIL: $1" >>"$verdict"; }
expect() {
    desc="$1"; expr="$2"
    state="$("$ctl" get state 2>>"$verdict")"
    if [ -z "$state" ]; then
        fail "$desc: weirctl get state returned nothing"
        return
    fi
    if printf '%s' "$state" | jq -e "$expr" >/dev/null 2>&1; then
        ok "$desc"
    else
        fail "$desc: jq '$expr' is false. state: $(printf '%s' "$state" | jq -c .)"
    fi
}

"$WEIR_TEST_DIR/weir" -log-level debug 2>"$WEIR_TEST_DIR/weir.log" &
sleep 1

expect "two outputs appear" '.outputs | length == 2'
expect "each output shows its own workspace" \
    '([.outputs[].workspace] | sort) == ["1", "2"]'
expect "outputs do not overlap" \
    '(.outputs[0].x + .outputs[0].width <= .outputs[1].x) or (.outputs[1].x + .outputs[1].width <= .outputs[0].x)'

# A window opens on the focused output's workspace.
"$FOOT" 2>/dev/null &
sleep 1
expect "window lands on the focused output's workspace" \
    '.windows[0].workspace == (.outputs[] | select(.focused) | .workspace)'

# Send it to the other output: it moves to that output's workspace and gets
# that output's geometry.
"$ctl" send-to-output next || fail "send-to-output"
expect "sent window is on the other output's workspace" \
    '.windows[0].workspace == (.outputs[] | select(.focused | not) | .workspace)'
expect "sent window is positioned within the other output" \
    '(.windows[0].x >= (.outputs[] | select(.focused | not) | .x)) and .windows[0].visible'

# Focus the other output and bring the window back.
"$ctl" focus-output next || fail "focus-output"
expect "focus-output moves output focus" \
    '(.outputs[] | select(.focused) | .workspace) == .windows[0].workspace'
"$ctl" send-to-output prev || fail "send-to-output prev"
"$ctl" focus-output prev || fail "focus-output prev"

# Locked mode: viewing a desktop switches every output at once.
"$ctl" workspace-mode locked || fail "workspace-mode"
"$ctl" view 3 || fail "view in locked mode"
expect "locked mode switches every output" \
    '[.outputs[].workspace | startswith("3@")] | all'
"$ctl" workspace-mode independent || fail "workspace-mode independent"
"$ctl" view 1 || fail "view 1"

# Output hotplug via wlr-randr, if available.
if [ -n "${WLR_RANDR:-}" ]; then
    second="$("$ctl" get outputs | jq -r '.[] | select(.focused | not) | .name')"
    onsecond="$("$ctl" get outputs | jq -r '.[] | select(.focused | not) | .workspace')"
    "$WLR_RANDR" --output "$second" --off 2>>"$verdict" || fail "wlr-randr --off"
    sleep 1
    expect "disabling an output removes it" '.outputs | length == 1'
    expect "no windows are lost when an output disappears" \
        '.windows | length == 1'
    "$WLR_RANDR" --output "$second" --on 2>>"$verdict" || fail "wlr-randr --on"
    sleep 1
    expect "re-enabling the output brings it back" '.outputs | length == 2'
    state="$("$ctl" get outputs)"
    if printf '%s' "$state" | jq -e --arg n "$second" --arg w "$onsecond" \
        '[.[] | select(.name == $n and .workspace == $w)] | length == 1' >/dev/null 2>&1; then
        ok "re-enabled output restores its workspace ($onsecond)"
    else
        fail "re-enabled output did not restore workspace $onsecond: $(printf '%s' "$state" | jq -c .)"
    fi
fi

echo done >>"$verdict"
"$ctl" exit
TESTEOF
chmod +x "$dir/test"

env -i \
    HOME="$dir" \
    PATH="$PATH" \
    WEIR_TEST_DIR="$dir" \
    FOOT="$FOOT" \
    WLR_RANDR="${WLR_RANDR:-}" \
    XDG_RUNTIME_DIR="$dir/run" \
    WLR_BACKENDS=headless \
    WLR_RENDERER=pixman \
    WLR_LIBINPUT_NO_DEVICES=1 \
    WLR_HEADLESS_OUTPUTS=2 \
    timeout --signal=KILL 30 \
    "$RIVER" -no-xwayland -log-level info -c "$dir/test" >"$dir/river.log" 2>&1 || true
pkill -TERM -f "$dir/weir" 2>/dev/null || true

echo "=== verdict ==="
cat "$dir/verdict" 2>/dev/null || { echo "FAIL: test produced no verdict"; tail -20 "$dir/river.log"; exit 1; }
echo
if ! grep -q "^done$" "$dir/verdict"; then
    echo "FAIL: test did not run to completion"
    echo "=== weir log ==="; tail -20 "$dir/weir.log" 2>/dev/null
    exit 1
fi
if grep -q "^FAIL" "$dir/verdict"; then
    echo "=== weir log ==="; tail -20 "$dir/weir.log" 2>/dev/null
    echo "RESULT: FAIL"
    exit 1
fi
echo "RESULT: PASS ($(grep -c '^ok' "$dir/verdict") assertions)"
