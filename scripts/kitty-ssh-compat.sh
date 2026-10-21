#!/bin/sh
# Opt-in proof that a direct Kitty window keeps the shell as Kitty's child,
# an SSH-aware split clones kitten ssh, and a Kitty overlay does not replace
# that foreground. Not run by task test or CI.
#
# Needs a display. Without DISPLAY and WAYLAND_DISPLAY it prints the manual
# steps and exits 0.
set -eu

if [ -z "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ]; then
  echo "no display; manual check:"
  echo "  1. kitty with allow_remote_control and listen_on"
  echo "  2. new window foreground is zsh, not remnix-attach"
  echo "  3. kitten ssh, then new_window_with_cwd opens another kitten ssh"
  echo "  4. Ctrl+R opens the panel and the shell window's foreground stays zsh"
  exit 0
fi

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
bin="$root/bin/remnix"
if [ ! -x "$bin" ]; then
  echo "build $bin first (make build)" >&2
  exit 1
fi
kitty=$(command -v kitty) || {
  echo "kitty is not installed" >&2
  exit 1
}

tmp=$(mktemp -d)
trap 'kill "$kitty_pid" 2>/dev/null || true; rm -rf "$tmp"' EXIT INT TERM
sock="$tmp/kitty.sock"
cfg="$tmp/config"
mkdir -p "$cfg"
cat >"$cfg/config.yaml" <<'EOF'
pty_proxy:
  enabled: true
  height: 40
EOF
cat >"$tmp/.zshrc" <<EOF
export PATH="$root/bin:\$PATH"
export REMNIX_CONFIG_DIR="$cfg"
export REMNIX_RUNTIME_DIR="$tmp/run"
eval "\$("$bin" init zsh)"
echo REMNIX_PTY_PROXY_MODE=\${REMNIX_PTY_PROXY_MODE-}
EOF

kitty_log="$tmp/kitty.log"
"$kitty" --listen-on "unix:$sock" --config NONE \
  --override "allow_remote_control=yes" \
  --override "shell=zsh" \
  --override "shell_integration=enabled" \
  --override "enabled_layouts=splits" \
  --class remnix-kitty-compat \
  >"$kitty_log" 2>&1 &
kitty_pid=$!

i=0
while [ ! -S "$sock" ]; do
  i=$((i + 1))
  if [ "$i" -gt 50 ]; then
    echo "kitty socket did not appear" >&2
    cat "$kitty_log" >&2 || true
    exit 1
  fi
  sleep 0.1
done

export KITTY_LISTEN_ON="unix:$sock"
at() { "$kitty" @ --to "unix:$sock" "$@"; }

# Replace the default shell's rc for the windows we launch.
launch_zsh() {
  at launch --type=window --env "ZDOTDIR=$tmp" --env "REMNIX_CONFIG_DIR=$cfg" --env "REMNIX_RUNTIME_DIR=$tmp/run" --cwd "$root" zsh -i
}

base=$(launch_zsh | tr -d '[:space:]')
sleep 0.4

fg_of() {
  at ls | python3 -c '
import json, sys
want = int(sys.argv[1])
data = json.load(sys.stdin)
for os_win in data:
    for tab in os_win.get("tabs", []):
        for win in tab.get("windows", []):
            if win.get("id") == want:
                procs = win.get("foreground_processes") or []
                cmd = procs[0].get("cmdline") if procs else []
                print(" ".join(cmd or []))
                raise SystemExit(0)
raise SystemExit(1)
' "$1"
}

base_fg=$(fg_of "$base" || true)
case "$base_fg" in
  *remnix-attach*|*pty-proxy*)
    echo "direct Kitty window foreground is the proxy:" >&2
    printf '%s\n' "$base_fg" >&2
    exit 1
    ;;
esac
case "$base_fg" in
  *zsh*) ;;
  *)
    echo "expected zsh as Kitty foreground, got:" >&2
    printf '%s\n' "$base_fg" >&2
    exit 1
    ;;
esac

# Overlay must not become the base window's foreground.
at focus-window --match "id:$base" >/dev/null
overlay=$(at launch --type=overlay --env "KITTY_LISTEN_ON=unix:$sock" --env "REMNIX_KITTY_PARENT=$base" --allow-remote-control \
  "$bin" search --kitty-panel --kitty-parent "$base" --interactive | tr -d '[:space:]')
sleep 0.8
if ! at ls | python3 -c '
import json, sys
want = int(sys.argv[1])
for os_win in json.load(sys.stdin):
    for tab in os_win.get("tabs", []):
        for win in tab.get("windows", []):
            if win.get("id") == want:
                raise SystemExit(0)
raise SystemExit(1)
' "$overlay"; then
  echo "kitty panel overlay exited immediately" >&2
  cat "$kitty_log" >&2 || true
  exit 1
fi
still=$(fg_of "$base" || true)
case "$still" in
  *remnix-attach*|*pty-proxy*|*remnix*)
    echo "opening the panel replaced the base foreground:" >&2
    printf '%s\n' "$still" >&2
    exit 1
    ;;
esac
at close-window --match "id:$overlay" >/dev/null 2>&1 || true

if ! command -v sshd >/dev/null 2>&1; then
  echo "sshd not installed; skipped the kitten ssh split"
  exit 0
fi

sshd=$(command -v sshd)
port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')
mkdir -p "$tmp/empty"
cat >"$tmp/sshd_config" <<EOF
Port $port
ListenAddress 127.0.0.1
HostKey $tmp/ssh_host_ed25519_key
PidFile $tmp/sshd.pid
UsePAM no
PasswordAuthentication no
KbdInteractiveAuthentication no
ChallengeResponseAuthentication no
PubkeyAuthentication yes
AuthorizedKeysFile $tmp/authorized_keys
StrictModes no
Subsystem sftp /usr/lib/ssh/sftp-server
EOF
ssh-keygen -q -t ed25519 -f "$tmp/ssh_host_ed25519_key" -N ""
ssh-keygen -q -t ed25519 -f "$tmp/id_ed25519" -N ""
cp "$tmp/id_ed25519.pub" "$tmp/authorized_keys"
"$sshd" -f "$tmp/sshd_config" -D -E "$tmp/sshd.log" &
sshd_pid=$!
trap 'kill "$sshd_pid" "$kitty_pid" 2>/dev/null || true; rm -rf "$tmp"' EXIT INT TERM
sleep 0.4

at send-text --match "id:$base" "kitten ssh -i $tmp/id_ed25519 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -p $port $(id -un)@127.0.0.1\r"
i=0
kitten=0
while [ "$i" -lt 40 ]; do
  i=$((i + 1))
  if fg_of "$base" | grep -q 'kitten ssh'; then
    kitten=1
    break
  fi
  sleep 0.25
done
if [ "$kitten" -ne 1 ]; then
  echo "kitten ssh did not become the foreground" >&2
  fg_of "$base" >&2 || true
  exit 1
fi
# The split needs the remote shell's reported cwd, which arrives with the prompt.
i=0
while [ "$i" -lt 24 ]; do
  i=$((i + 1))
  if at get-text --match "id:$base" | grep -q '127.0.0.1'; then
    break
  fi
  sleep 0.25
done

count_kittens() {
  at ls | python3 -c '
import json, sys
n = 0
for os_win in json.load(sys.stdin):
    for tab in os_win.get("tabs", []):
        for win in tab.get("windows", []):
            for p in win.get("foreground_processes") or []:
                cmd = p.get("cmdline") or []
                if cmd and cmd[0].endswith("kitten") and len(cmd) > 1 and cmd[1] == "ssh":
                    n += 1
print(n)
'
}
at focus-window --match "id:$base" >/dev/null
before=$(count_kittens)
at launch --cwd=current
sleep 1.5
after=$(count_kittens)
if [ "$after" -le "$before" ]; then
  echo "cwd=current split did not open a second kitten ssh (had $before, now $after)" >&2
  exit 1
fi
echo "kitty ssh split and overlay panel: ok"
