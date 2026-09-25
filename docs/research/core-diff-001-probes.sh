#!/bin/sh
# Reproduce the central CORE-DIFF-001 behavior/security probes.
# This creates and removes only /tmp/kit-core-diff-probe-* directories/files.
set -eu

ROOT=/tmp/kit-core-diff-probe-repo
SUB=/tmp/kit-core-diff-probe-sub
UNBORN=/tmp/kit-core-diff-probe-unborn
PWN=/tmp/kit-core-diff-probe-filter-ran
rm -rf "$ROOT" "$SUB" "$UNBORN" "$PWN"
trap 'rm -rf "$ROOT" "$SUB" "$UNBORN" "$PWN"' EXIT

printf 'git=%s\ngo=%s\n' "$(git --version)" "$(go version)"

git init -q "$SUB"
git -C "$SUB" config user.email probe@example.invalid
git -C "$SUB" config user.name Probe
printf 'sub\n' >"$SUB/s"
git -C "$SUB" add s
git -C "$SUB" commit -qm init

git init -q "$ROOT"
git -C "$ROOT" config user.email probe@example.invalid
git -C "$ROOT" config user.name Probe
printf 'one\ntwo\nthree\n' >"$ROOT/both.txt"
printf 'rename\n' >"$ROOT/old.txt"
printf 'mode\n' >"$ROOT/mode.sh"
printf 'target-a' >"$ROOT/target-a"
ln -s target-a "$ROOT/link"
printf '*.ignored\n' >"$ROOT/.gitignore"
git -C "$ROOT" add .
git -C "$ROOT" -c protocol.file.allow=always submodule add -q "$SUB" sub
git -C "$ROOT" commit -qm base

printf 'one\nTWO-STAGED\nthree\n' >"$ROOT/both.txt"
git -C "$ROOT" add both.txt
printf 'one\nTWO-STAGED\nTHREE-WT\n' >"$ROOT/both.txt"
mv "$ROOT/old.txt" "$ROOT/new.txt"
chmod +x "$ROOT/mode.sh"
rm "$ROOT/link"
ln -s target-b "$ROOT/link"
printf 'hello untracked\n' >"$ROOT/new-untracked.txt"
printf x >"$ROOT/no.ignored"
printf '\000\001binary' >"$ROOT/new.bin"
printf dirty >>"$ROOT/sub/s"

printf '\n# porcelain v2 (NUL rendered by Python repr)\n'
git -C "$ROOT" --no-optional-locks -c core.quotepath=false \
  status --porcelain=v2 -z --untracked-files=all --ignored=no --no-renames |
  python3 -c 'import sys; print(repr(sys.stdin.buffer.read()))'

printf '\n# raw HEAD diff (untracked files are absent)\n'
git -C "$ROOT" --no-optional-locks -c core.quotepath=false \
  diff HEAD --raw -z --no-ext-diff --no-textconv --no-renames \
  --ignore-submodules=none |
  python3 -c 'import sys; print(repr(sys.stdin.buffer.read()))'

printf '\n# tracked plus eligible untracked path discovery\n'
git -C "$ROOT" --no-optional-locks -c core.quotepath=false \
  -c core.excludesFile=/dev/null \
  ls-files -z --cached --others --exclude-standard |
  python3 -c 'import sys; print(repr(sys.stdin.buffer.read()))'

printf '\n# repository-configured fsmonitor runs from read-only plumbing\n'
cat >"$ROOT/fsmonitor-probe.sh" <<EOF
#!/bin/sh
touch "$PWN"
exit 0
EOF
chmod +x "$ROOT/fsmonitor-probe.sh"
git -C "$ROOT" config core.fsmonitor "$ROOT/fsmonitor-probe.sh"
rm -f "$PWN"
git -C "$ROOT" ls-files --stage >/dev/null
if test -e "$PWN"; then
  echo 'FSMONITOR_EXECUTED=yes'
else
  echo 'FSMONITOR_EXECUTED=no (unexpected)'
  exit 1
fi
rm -f "$PWN"
git -C "$ROOT" -c core.fsmonitor=false ls-files --stage >/dev/null
if test -e "$PWN"; then
  echo 'FSMONITOR_OVERRIDE_BLOCKED=no (unexpected)'
  exit 1
else
  echo 'FSMONITOR_OVERRIDE_BLOCKED=yes'
fi
git -C "$ROOT" config --unset core.fsmonitor

printf '\n# hostile clean-filter execution despite diff safety flags\n'
printf '*.txt filter=evil diff=evil\n' >"$ROOT/.gitattributes"
git -C "$ROOT" config filter.evil.clean \
  "sh -c 'touch $PWN; cat'"
git -C "$ROOT" config diff.evil.command \
  "sh -c 'touch $PWN'"
git -C "$ROOT" diff HEAD --no-ext-diff --no-textconv -- both.txt >/dev/null
if test -e "$PWN"; then
  echo 'FILTER_EXECUTED=yes'
else
  echo 'FILTER_EXECUTED=no (unexpected)'
  exit 1
fi

printf '\n# status can also execute a filter when content checking is required\n'
rm -f "$PWN"
git -C "$ROOT" -c filter.evil.clean= add .gitattributes both.txt
git -C "$ROOT" commit -qm filter-baseline
sleep 1
touch "$ROOT/both.txt"
git -C "$ROOT" status --porcelain=v2 -z >/dev/null
if test -e "$PWN"; then
  echo 'STATUS_FILTER_EXECUTED=yes'
else
  echo 'STATUS_FILTER_EXECUTED=no (Git/filesystem may have reused stat data)'
fi

printf '\n# unborn repository\n'
git init -q "$UNBORN"
printf hi >"$UNBORN/a"
if git -C "$UNBORN" rev-parse --verify 'HEAD^{commit}' >/dev/null 2>&1; then
  echo 'UNBORN=no (unexpected)'
  exit 1
else
  echo 'UNBORN=yes'
fi
git -C "$UNBORN" -c core.excludesFile=/dev/null \
  ls-files -z --stage --others --exclude-standard |
  python3 -c 'import sys; print(repr(sys.stdin.buffer.read()))'

printf '\nProbe complete. See the report for library/API and size measurements.\n'
