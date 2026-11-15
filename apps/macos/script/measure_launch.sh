#!/usr/bin/env bash
# Launches a channel-bound development bundle repeatedly and reports how long
# each launch milestone took (see Sources/Kit/Support/LaunchMetrics.swift).
#
#   apps/macos/script/measure_launch.sh --app PATH [--runs N] [--state FILE|none]
#     [--floor] [--cold] [--json FILE] [--timeout SECONDS]
#
# Times are from the `open` call, which stands in for the Dock click, to each
# milestone; the launch ends at firstContentFrame.
#
# --state copies FILE over the bundle's saved windows (windows.json) before
#   each launch, so every launch restores the same tabs; "none" removes them,
#   for a launch with no saved windows.
# --floor also launches an empty SwiftUI app (launch_floor.swift) before each
#   of the bundle's launches, as the floor this Mac sets under its current load.
# --cold runs `sudo purge` before each launch to drop the file cache, and asks
#   for a password.
#
# The bundle must be bound to a channel (its Info.plist names KitDevHome) and
# carry its own identifier (com.akonwi.kit.dev.NAME, from KIT_MACOS_BUNDLE_ID),
# so relaunching it never touches ~/.kit, an installed Kit.app, or the Kit Dev
# in everyday use. It is quit between launches by its path, as Quit does, so
# it saves its windows. Each launch makes it the active app.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP=""
RUNS=10
COLD=0
FLOOR=0
JSON=""
STATE=""
TIMEOUT=30
usage() { echo "usage: $0 --app PATH [--runs N] [--state FILE|none] [--floor] [--cold] [--json FILE] [--timeout SECONDS]" >&2; exit 2; }
while [[ $# -gt 0 ]]; do
  case "$1" in
    --app) APP="$2"; shift 2 ;;
    --runs) RUNS="$2"; shift 2 ;;
    --cold) COLD=1; shift ;;
    --floor) FLOOR=1; shift ;;
    --json) JSON="$2"; shift 2 ;;
    --state) STATE="$2"; shift 2 ;;
    --timeout) TIMEOUT="$2"; shift 2 ;;
    *) usage ;;
  esac
done
[[ -n "$APP" && -d "$APP" ]] || { echo "No bundle at '$APP'" >&2; usage; }
[[ "$RUNS" =~ ^[1-9][0-9]*$ ]] || usage
APP="$(cd "$APP" && pwd)"
PLIST="$APP/Contents/Info.plist"
BUNDLE_ID="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$PLIST")"
DEV_HOME="$(/usr/libexec/PlistBuddy -c 'Print :KitDevHome' "$PLIST" 2>/dev/null || true)"
[[ "$BUNDLE_ID" =~ ^com\.akonwi\.kit\.dev\.[A-Za-z0-9-]+$ ]] || {
  echo "Refusing $BUNDLE_ID: build with KIT_MACOS_BUNDLE_ID=com.akonwi.kit.dev.NAME" >&2; exit 2; }
[[ -n "$DEV_HOME" && "$DEV_HOME" != "$HOME/.kit" ]] || {
  echo "Refusing a bundle without a channel home: build with KIT_MACOS_CHANNEL=NAME" >&2; exit 2; }
[[ -f "$DEV_HOME/run/server.json" ]] || echo "warning: no server is registered in $DEV_HOME" >&2
[[ -z "$STATE" || "$STATE" == none || -f "$STATE" ]] || { echo "No state file at '$STATE'" >&2; exit 2; }

FLOOR_APP=""
if [[ "$FLOOR" == 1 ]]; then
  FLOOR_APP="${TMPDIR:-/tmp}/kit-launch-floor/Floor.app"
  if [[ ! -x "$FLOOR_APP/Contents/MacOS/Floor" || "$SCRIPT_DIR/launch_floor.swift" -nt "$FLOOR_APP/Contents/MacOS/Floor" ]]; then
    mkdir -p "$FLOOR_APP/Contents/MacOS"
    xcrun swiftc -O -parse-as-library "$SCRIPT_DIR/launch_floor.swift" -o "$FLOOR_APP/Contents/MacOS/Floor"
    cat > "$FLOOR_APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>Floor</string>
<key>CFBundleIdentifier</key><string>com.akonwi.kit.dev.launchfloor</string>
<key>CFBundleName</key><string>Launch Floor</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>LSMinimumSystemVersion</key><string>15.0</string>
<key>NSPrincipalClass</key><string>NSApplication</string>
</dict></plist>
PLIST
    codesign --force --sign - "$FLOOR_APP" 2>/dev/null
  fi
fi
[[ "$COLD" == 0 ]] || sudo -v

exec python3 - "$APP" "$RUNS" "$COLD" "$TIMEOUT" "$JSON" "$DEV_HOME" "$BUNDLE_ID" "$STATE" "$FLOOR_APP" <<'PY'
import json, os, shutil, statistics, subprocess, sys, tempfile, time

app, runs, cold, timeout, json_out, home = sys.argv[1], int(sys.argv[2]), sys.argv[3] == "1", float(sys.argv[4]), sys.argv[5], sys.argv[6]
bundle_id, state, floor_app = sys.argv[7], sys.argv[8], sys.argv[9]
saved_windows = os.path.expanduser(f"~/Library/Application Support/{bundle_id}/windows.json")
order = ["processStart", "main", "appInit", "delegateInit", "willFinishLaunching", "windowContent", "didFinishLaunching",
         "firstWindow", "windowVisible", "serverConnected", "sessionDelivered", "sessionShown", "contentReady",
         "firstContentFrame", "composerFocused"]
phases = [("LaunchServices → exec", None, "processStart"), ("exec → main (dyld, init)", "processStart", "main"),
          ("main → delegate", "main", "delegateInit"), ("delegate → willFinishLaunching", "delegateInit", "willFinishLaunching"),
          ("willFinish → didFinishLaunching", "willFinishLaunching", "didFinishLaunching"),
          ("didFinishLaunching → window", "didFinishLaunching", "windowVisible"),
          ("window → session delivered", "windowVisible", "sessionDelivered"),
          ("delivered → session shown", "sessionDelivered", "sessionShown"),
          ("session shown → content ready", "sessionShown", "contentReady"),
          ("content ready → frame", "contentReady", "firstContentFrame")]

def executable(bundle):
    with open(os.path.join(bundle, "Contents/Info.plist"), "rb") as f:
        import plistlib
        return os.path.join(bundle, "Contents/MacOS", plistlib.load(f)["CFBundleExecutable"])

def pids(bundle):
    out = subprocess.run(["pgrep", "-f", "^" + executable(bundle) + "$"], capture_output=True, text=True).stdout
    return [int(p) for p in out.split()]

def quit_app(bundle, deadline=10):
    if not pids(bundle):
        return
    # Quit as the menu does, so the app saves its windows. A terminal still
    # starting its shell can make it ask first; then it is terminated.
    try:
        subprocess.run(["osascript", "-e", f'tell application "{bundle}" to quit'], capture_output=True, timeout=deadline)
    except subprocess.TimeoutExpired:
        pass
    end = time.time() + deadline
    while pids(bundle) and time.time() < end:
        time.sleep(0.1)
    for signal in ("-TERM", "-KILL"):
        if not pids(bundle):
            return
        print(f"{bundle} did not quit; sending {signal[1:]}", file=sys.stderr)
        subprocess.run(["kill", signal] + [str(p) for p in pids(bundle)])
        time.sleep(2)

def launch(bundle, report):
    """Launches the bundle once and returns each milestone in ms from `open`."""
    if cold:
        subprocess.run(["sudo", "purge"], check=True)
        time.sleep(1)
    start = time.time()
    subprocess.run(["open", "-n", "--env", f"KIT_LAUNCH_REPORT={report}", bundle], check=True)
    end = start + timeout
    while not os.path.exists(report) and time.time() < end:
        time.sleep(0.02)
    if not os.path.exists(report):
        quit_app(bundle)
        sys.exit(f"{bundle}: no launch report within {timeout:.0f}s")
    time.sleep(0.05)
    with open(report) as f:
        data = json.load(f)
    launch_services = (data["processStart"] - start) * 1000
    times = {"processStart": launch_services}
    for name, ms in data["milestonesMs"].items():
        times[name] = launch_services + ms
    # Let the launch's own work finish before quitting, and the quit's
    # before the next launch, so launches don't overlap.
    time.sleep(1)
    quit_app(bundle)
    time.sleep(2)
    return times

def pct(values, p):
    values = sorted(values)
    k = (len(values) - 1) * p
    lo = int(k)
    hi = min(lo + 1, len(values) - 1)
    return values[lo] + (values[hi] - values[lo]) * (k - lo)

def stats(values):
    return {"min": min(values), "median": statistics.median(values), "p90": pct(values, 0.9), "n": len(values)}

for bundle in filter(None, [app, floor_app]):
    if pids(bundle):
        sys.exit(f"{bundle} is already running; quit it first")

print(f"bundle {app}\nhome   {home}\nruns   {runs} ({'cold: sudo purge before each' if cold else 'warm'})"
      + (f", each after a launch of {floor_app}" if floor_app else ""), file=sys.stderr)
results, floors = [], []
report_dir = tempfile.mkdtemp(prefix="kit-launch-")
for run in range(runs):
    if floor_app:
        floors.append(launch(floor_app, os.path.join(report_dir, f"floor{run}.json")))
    if state == "none":
        if os.path.exists(saved_windows):
            os.remove(saved_windows)
    elif state:
        os.makedirs(os.path.dirname(saved_windows), exist_ok=True)
        shutil.copyfile(state, saved_windows)
    times = launch(app, os.path.join(report_dir, f"run{run}.json"))
    results.append(times)
    line = f"run {run + 1}: firstContentFrame {times.get('firstContentFrame', float('nan')):.0f} ms"
    if floor_app:
        line += f" (floor {floors[-1].get('firstContentFrame', float('nan')):.0f} ms)"
    print(line, file=sys.stderr)

summary = {}
print(f"\n{'milestone (ms from open)':<26}{'min':>8}{'median':>8}{'p90':>8}{'n':>4}")
for name in order:
    values = [r[name] for r in results if name in r]
    if values:
        summary[name] = s = stats(values)
        print(f"{name:<26}{s['min']:>8.0f}{s['median']:>8.0f}{s['p90']:>8.0f}{s['n']:>4}")

print(f"\n{'phase (ms)':<34}{'min':>8}{'median':>8}{'p90':>8}")
for label, a, b in phases:
    values = [r[b] - (r[a] if a else 0) for r in results if b in r and (a is None or a in r)]
    if values:
        print(f"{label:<34}{min(values):>8.0f}{statistics.median(values):>8.0f}{pct(values, 0.9):>8.0f}")

floor_summary = None
if floors:
    floor_values = [r["firstContentFrame"] for r in floors]
    floor_summary = stats(floor_values)
    over = [r["firstContentFrame"] - f["firstContentFrame"] for r, f in zip(results, floors) if "firstContentFrame" in r]
    s = floor_summary
    print(f"\n{'empty SwiftUI app (floor)':<34}{s['min']:>8.0f}{s['median']:>8.0f}{s['p90']:>8.0f}")
    print(f"{'bundle minus floor, per run':<34}{min(over):>8.0f}{statistics.median(over):>8.0f}{pct(over, 0.9):>8.0f}")
if json_out:
    with open(json_out, "w") as f:
        json.dump({"bundle": app, "cold": cold, "runs": results, "summary": summary,
                   "floorRuns": floors, "floor": floor_summary}, f, indent=2)
PY
