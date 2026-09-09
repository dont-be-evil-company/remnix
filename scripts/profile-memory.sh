#!/usr/bin/env bash
# Record remnix RSS/PSS for 1, 5, 10, and 20 idle attached shells.
# Usage:
#   scripts/profile-memory.sh [legacy|daemon] [counts...]
#   REMNIX_BIN=./bin/remnix scripts/profile-memory.sh
#
# legacy  - current architecture: agent + N pty-proxy processes (default)
# daemon  - unified daemon + N remnix-attach helpers

set -euo pipefail

mode="${1:-legacy}"
if [[ "$mode" == "legacy" || "$mode" == "daemon" ]]; then
  shift || true
else
  mode="legacy"
fi
counts=("${@:-1 5 10 20}")

root="$(cd "$(dirname "$0")/.." && pwd)"
bin="${REMNIX_BIN:-$root/bin/remnix}"
attach_bin="${REMNIX_ATTACH_BIN:-$root/bin/remnix-attach}"
runtime="${REMNIX_PROFILE_RUNTIME:-$(mktemp -d -t remnix-profile.XXXXXX)}"
workdir="${REMNIX_PROFILE_WORKDIR:-$(mktemp -d -t remnix-profile-work.XXXXXX)}"
shell_bin="${REMNIX_PROFILE_SHELL:-/bin/sh}"
heap="${REMNIX_HEAP_PROFILE:-}"

cleanup() {
  if [[ -n "${child_pids:-}" ]]; then
    # shellcheck disable=SC2086
    kill $child_pids 2>/dev/null || true
  fi
  if [[ -n "${daemon_pid:-}" ]]; then
    kill "$daemon_pid" 2>/dev/null || true
  fi
  if [[ -n "${agent_pid:-}" ]]; then
    kill "$agent_pid" 2>/dev/null || true
  fi
  rm -rf "$runtime" "$workdir"
}
trap cleanup EXIT

if [[ ! -x "$bin" ]]; then
  echo "building $bin" >&2
  (cd "$root" && go build -tags piv -o "$bin" ./cmd/remnix)
fi

export REMNIX_RUNTIME_DIR="$runtime"
export REMNIX_DATA_DIR="${REMNIX_DATA_DIR:-$workdir/data}"
export REMNIX_CONFIG_DIR="${REMNIX_CONFIG_DIR:-$workdir/cfg}"
mkdir -p "$REMNIX_DATA_DIR" "$REMNIX_CONFIG_DIR" "$runtime"

rss_kb() {
  local pid="$1"
  if [[ -r "/proc/$pid/status" ]]; then
    awk '/^VmRSS:/ {print $2}' "/proc/$pid/status"
  else
    ps -o rss= -p "$pid" 2>/dev/null | tr -d ' '
  fi
}

pss_kb() {
  local pid="$1"
  if [[ -r "/proc/$pid/smaps_rollup" ]]; then
    awk '/^Pss:/ {print $2; exit}' "/proc/$pid/smaps_rollup"
  else
    echo ""
  fi
}

cmdline() {
  local pid="$1"
  if [[ -r "/proc/$pid/cmdline" ]]; then
    tr '\0' ' ' <"/proc/$pid/cmdline"
    echo
  else
    ps -o args= -p "$pid" 2>/dev/null || true
  fi
}

maybe_heap() {
  local pid="$1" label="$2"
  [[ -n "$heap" ]] || return 0
  if command -v go >/dev/null 2>&1; then
    echo "heap profile requested for $label pid=$pid (set GOTRACEBACK / pprof manually)" >&2
  fi
}

print_proc() {
  local role="$1" pid="$2"
  local rss pss
  rss="$(rss_kb "$pid")"
  pss="$(pss_kb "$pid")"
  printf '  %-10s pid=%-8s rss_kb=%-8s pss_kb=%-8s %s\n' "$role" "$pid" "${rss:-?}" "${pss:-n/a}" "$(cmdline "$pid")"
}

sum_rss() {
  local total=0 pid rss
  for pid in "$@"; do
    [[ -n "$pid" ]] || continue
    rss="$(rss_kb "$pid")"
    if [[ -n "$rss" ]]; then
      total=$((total + rss))
    fi
  done
  echo "$total"
}

start_legacy_agent() {
  "$bin" agent >/dev/null 2>&1 &
  agent_pid=$!
  local i
  for i in $(seq 1 50); do
    [[ -S "$runtime/agent.sock" || -S "$runtime/control.sock" ]] && return 0
    sleep 0.05
  done
}

start_unified_daemon() {
  "$bin" daemon >/dev/null 2>&1 &
  daemon_pid=$!
  local i
  for i in $(seq 1 80); do
    [[ -S "$runtime/control.sock" ]] && return 0
    sleep 0.05
  done
}

start_idle_shells() {
  local n="$1" i
  child_pids=""
  for i in $(seq 1 "$n"); do
    if [[ "$mode" == "daemon" && -x "$attach_bin" ]]; then
      "$attach_bin" --shell "$shell_bin" >/dev/null 2>&1 &
    elif [[ "$mode" == "daemon" ]]; then
      script -q -c "$shell_bin" /dev/null >/dev/null 2>&1 &
    else
      script -q -c "$bin pty-proxy --shell $shell_bin" /dev/null >/dev/null 2>&1 &
    fi
    child_pids+=" $!"
  done
  sleep 0.4
}

echo "mode=$mode bin=$bin runtime=$runtime"
echo "counts=${counts[*]}"
echo

if [[ "$mode" == "daemon" ]]; then
  start_unified_daemon || echo "warning: daemon socket not ready" >&2
else
  start_legacy_agent || echo "warning: agent socket not ready" >&2
fi

printf '%-10s %-14s %-16s %-16s %-16s\n' "terminals" "daemon_rss_kb" "attach_rss_kb" "proxy_rss_kb" "total_remnix_kb"
printf '%s\n' "----------------------------------------------------------------------"

for n in "${counts[@]}"; do
  if [[ -n "${child_pids:-}" ]]; then
    # shellcheck disable=SC2086
    kill $child_pids 2>/dev/null || true
    child_pids=""
    sleep 0.2
  fi
  start_idle_shells "$n"

  heavy_pids=()
  attach_pids=()
  proxy_pids=()
  while read -r pid; do
    [[ -n "$pid" ]] || continue
    cmd="$(cmdline "$pid")"
    case "$cmd" in
      *' remnix daemon'*|*' daemon') heavy_pids+=("$pid") ;;
      *' remnix agent'*|*' agent') heavy_pids+=("$pid") ;;
      *' remnix pty-proxy'*|*' pty-proxy '*) proxy_pids+=("$pid"); heavy_pids+=("$pid") ;;
      *remnix-attach*) attach_pids+=("$pid") ;;
    esac
  done < <(pgrep -f 'remnix|remnix-attach' || true)

  echo
  echo "=== $n terminals ==="
  for pid in "${heavy_pids[@]:-}"; do
    print_proc "heavy" "$pid"
    maybe_heap "$pid" "heavy"
  done
  for pid in "${attach_pids[@]:-}"; do
    print_proc "attach" "$pid"
  done
  for pid in "${proxy_pids[@]:-}"; do
    print_proc "proxy" "$pid"
  done

  daemon_rss="$(sum_rss "${daemon_pid:-}" "${agent_pid:-}")"
  # Prefer classified pids when pgrep found them.
  if [[ ${#heavy_pids[@]} -gt 0 ]]; then
    daemon_rss="$(sum_rss "${heavy_pids[@]}")"
  fi
  attach_rss="$(sum_rss "${attach_pids[@]:-}")"
  proxy_rss="$(sum_rss "${proxy_pids[@]:-}")"
  total=$((daemon_rss + attach_rss))
  printf '%-10s %-14s %-16s %-16s %-16s\n' "$n" "$daemon_rss" "$attach_rss" "$proxy_rss" "$total"
done
