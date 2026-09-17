#!/bin/sh
# Reproducible CORE-DIFF-001 edge/security probes. Creates only a private temp tree.
set -eu
umask 077
BASE=$(mktemp -d "${TMPDIR:-/tmp}/kit-core-diff-followup.XXXXXX")
trap 'rm -rf "$BASE"' EXIT HUP INT TERM
# Isolate fixtures from caller Git configuration and Git-specific environment.
for name in $(env | sed -n 's/^\(GIT_[A-Za-z0-9_]*\)=.*/\1/p'); do unset "$name"; done
mkdir "$BASE/home" "$BASE/xdg"
export HOME="$BASE/home" XDG_CONFIG_HOME="$BASE/xdg" GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null
export LC_ALL=C GIT_TERMINAL_PROMPT=0 GIT_NO_LAZY_FETCH=1 GIT_NO_REPLACE_OBJECTS=1 GIT_OPTIONAL_LOCKS=0
mark="$BASE/executed"
fail() { echo "FAIL: $*" >&2; exit 1; }
assert_has() { case "$1" in *"$2"*) ;; *) fail "expected [$2] in [$1]";; esac; }
init() { git init -q "$1"; git -C "$1" config user.name Probe; git -C "$1" config user.email probe@example.invalid; }
commit_all() { git -C "$1" add -A; git -C "$1" commit -qm "$2"; }

printf 'platform=%s\ngit=%s\n' "$(uname -srm)" "$(git --version)"

# Index edge states.
r="$BASE/index"; init "$r"
printf 'base\n' >"$r/tracked"; printf 'gone\n' >"$r/reverted"; printf 'hint\n' >"$r/hint"; commit_all "$r" base
git -C "$r" update-index --assume-unchanged hint
printf 'stage\n' >"$r/reverted"; git -C "$r" add reverted; printf 'gone\n' >"$r/reverted"
printf 'ita\n' >"$r/ita"; git -C "$r" add -N ita
idx=$(git -C "$r" -c core.fsmonitor=false ls-files --stage -v)
printf '\n[index]\n%s\n' "$idx"
assert_has "$idx" "	ita"; assert_has "$idx" 'h 100644'
git -C "$r" config diff.renames true
ita_hidden=$(git -C "$r" diff --cached --raw -z --abbrev=64 --no-renames --no-ext-diff --no-textconv --ita-invisible-in-index HEAD | python3 -c 'import sys; print(repr(sys.stdin.buffer.read()))')
ita_visible=$(git -C "$r" diff --cached --raw -z --abbrev=64 --no-renames --no-ext-diff --no-textconv --ita-visible-in-index HEAD | python3 -c 'import sys; print(repr(sys.stdin.buffer.read()))')
printf '[intent-to-add] hidden=%s visible=%s\n' "$ita_hidden" "$ita_visible"
case "$ita_hidden" in *ita*) fail 'ITA unexpectedly visible' ;; esac
assert_has "$ita_visible" 'ita'
test -z "$(git -C "$r" diff HEAD -- reverted)" || fail 'staged-then-reverted must be HEAD-to-live unchanged'

# A real three-stage conflict.
printf 'left\n' >"$r/conflict"; commit_all "$r" conflict-base
basebranch=$(git -C "$r" symbolic-ref --short HEAD)
git -C "$r" checkout -qb side; printf 'side\n' >"$r/conflict"; commit_all "$r" side
git -C "$r" checkout -q "$basebranch"; printf 'main\n' >"$r/conflict"; commit_all "$r" main
if git -C "$r" merge side >/dev/null 2>&1; then fail 'expected conflict'; fi
conf=$(git -C "$r" -c core.fsmonitor=false ls-files --stage -- conflict)
printf '[conflict]\n%s\n' "$conf"
for stage in 1 2 3; do printf '%s\n' "$conf" | grep -q " $stage[[:space:]]" || fail "missing stage $stage"; done
git -C "$r" merge --abort

# Unborn behavior.
u="$BASE/unborn"; init "$u"; printf x >"$u/ita"; git -C "$u" add -N ita
if git -C "$u" rev-parse --verify 'HEAD^{commit}' >/dev/null 2>&1; then fail 'expected unborn'; fi
printf '[unborn-stage]\n'; git -C "$u" ls-files --stage

# Cone sparse checkout and sparse-index directory entry.
s="$BASE/sparse"; init "$s"; mkdir -p "$s/a" "$s/b/sub"; printf a >"$s/a/f"; printf b >"$s/b/sub/f"; commit_all "$s" base
git -C "$s" sparse-checkout init --cone --sparse-index; git -C "$s" sparse-checkout set a
sparse=$(git -C "$s" -c core.fsmonitor=false ls-files --sparse --stage -t)
printf '\n[sparse-cone]\n%s\n' "$sparse"
assert_has "$sparse" '040000'; assert_has "$sparse" "	b/"
git -C "$s" sparse-checkout disable
# Non-cone and ordinary index still carry skip-worktree entries.
git -C "$s" sparse-checkout init --no-cone; printf '/a/f\n' >"$s/.git/info/sparse-checkout"; git -C "$s" read-tree -mu HEAD
noncone=$(git -C "$s" -c core.fsmonitor=false ls-files --stage -t)
printf '[sparse-non-cone]\n%s\n' "$noncone"
assert_has "$noncone" 'S 100644'

# Attribute/config matrix. Markers prove selected plumbing does not invoke helpers.
a="$BASE/attrs"; init "$a"
printf 'one\ntwo\n' >"$a/f.txt"; commit_all "$a" base
printf '*.txt text eol=crlf ident filter=evil diff=evil working-tree-encoding=UTF-16\n' >"$a/.gitattributes"
git -C "$a" add .gitattributes; git -C "$a" commit -qm attributes
cat >"$a/evil.sh" <<EOF
#!/bin/sh
touch "$mark"
cat
EOF
chmod +x "$a/evil.sh"
git -C "$a" config filter.evil.clean "$a/evil.sh"
git -C "$a" config filter.evil.smudge "$a/evil.sh"
git -C "$a" config diff.evil.textconv "$a/evil.sh"
git -C "$a" config diff.evil.command "$a/evil.sh"
git -C "$a" config core.fsmonitor "$a/evil.sh"
git -C "$a" config core.pager "$a/evil.sh"
git -C "$a" config credential.helper "!$a/evil.sh"
git -C "$a" config alias.ls-files "!$a/evil.sh"
mkdir "$a/hooks"
for hook in pre-commit post-commit pre-merge-commit post-merge pre-rebase post-checkout post-index-change; do cp "$a/evil.sh" "$a/hooks/$hook"; done
git -C "$a" config core.hooksPath "$a/hooks"
rm -f "$mark"
attrs=$(printf 'f.txt\0' | git -C "$a" --no-pager -c core.fsmonitor=false -c core.attributesFile=/dev/null check-attr -z --all --stdin | python3 -c 'import sys; print(repr(sys.stdin.buffer.read()))')
tree=$(git -C "$a" --no-pager -c core.fsmonitor=false ls-tree -r -z --full-tree HEAD | python3 -c 'import sys; print(repr(sys.stdin.buffer.read()))')
idx2=$(git -C "$a" --no-pager -c core.fsmonitor=false ls-files --stage -v -z | python3 -c 'import sys; print(repr(sys.stdin.buffer.read()))')
printf 'ita' >"$a/ita.dat"; git -C "$a" -c core.fsmonitor=false add -N ita.dat
# Setup may apply repository rules; only the following read-only commands are
# under the no-execution assertion.
rm -f "$mark"
ita_base=$(git -C "$a" -c core.fsmonitor=false rev-parse HEAD)
git -C "$a" --no-pager -c core.fsmonitor=false diff --cached --raw -z --abbrev=64 --no-renames --no-ext-diff --no-textconv --ita-invisible-in-index "$ita_base" >/dev/null
git -C "$a" --no-pager -c core.fsmonitor=false diff --cached --raw -z --abbrev=64 --no-renames --no-ext-diff --no-textconv --ita-visible-in-index "$ita_base" >/dev/null
printf '\n[attributes]\n%s\n[tree]\n%s\n[index-safe]\n%s\n' "$attrs" "$tree" "$idx2"
test ! -e "$mark" || fail 'safe plumbing executed configured helper'
# Demonstrate that worktree diff remains forbidden. Remove the encoding gate so
# the configured clean filter is the first conversion, then force content work.
printf '*.txt text filter=evil diff=evil\n' >"$a/.gitattributes"
printf 'changed\n' >>"$a/f.txt"
git -C "$a" -c core.fsmonitor=false diff HEAD --no-ext-diff --no-textconv -- f.txt >/dev/null || true
test -e "$mark" || fail 'expected forbidden worktree diff to execute clean filter'
rm -f "$mark"

# Hostile include is visible without following it when --no-includes is used.
printf '[include]\n[include]\n\tpath = %s\n' "$BASE/outside.cfg" >>"$a/.git/config"
printf '[core]\n\tfsmonitor = %s\n' "$a/evil.sh" >"$BASE/outside.cfg"
localcfg=$(git config --file "$a/.git/config" --no-includes --null --list | python3 -c 'import sys; print(repr(sys.stdin.buffer.read()))')
printf '%s\n' "$localcfg"
assert_has "$localcfg" 'include.path'

# Linked worktree/gitfile/common-dir shape.
l="$BASE/linked-main"; w="$BASE/linked-worktree"; init "$l"; printf x >"$l/f"; commit_all "$l" base
git -C "$l" worktree add -q -b linked "$w"
printf '\n[linked]\n'; printf 'gitfile='; cat "$w/.git"; printf 'gitdir=%s\ncommon=%s\n' "$(git -C "$w" rev-parse --absolute-git-dir)" "$(git -C "$w" rev-parse --git-common-dir)"

# core.worktree can redirect implicit repository commands; explicit validated
# --git-dir/--work-tree restores the admitted content root. Production also rejects it.
cw="$BASE/core-worktree"; outside="$BASE/outside-worktree"; init "$cw"; mkdir "$outside"; printf in >"$cw/f"; commit_all "$cw" base
git --git-dir="$cw/.git" config core.worktree "$outside"
redirected=$(git --git-dir="$cw/.git" rev-parse --show-toplevel)
explicit=$(git --git-dir="$cw/.git" --work-tree="$cw" rev-parse --show-toplevel)
printf '[core.worktree] redirected=%s explicit=%s\n' "$redirected" "$explicit"
test "$redirected" != "$explicit" || fail 'core.worktree escape probe did not redirect'

# Alternates, replace refs, partial-clone marker, and nested repository/submodule boundaries.
alt="$BASE/alternate"; init "$alt"; printf different >"$alt/f"; commit_all "$alt" base
printf '%s\n' "$alt/.git/objects" >"$l/.git/objects/info/alternates"
printf '[alternates]=present\n'
orig=$(git -C "$l" rev-parse HEAD); replacement=$(git -C "$alt" rev-parse HEAD); git -C "$l" replace "$orig" "$replacement"
no_replace=$(GIT_NO_REPLACE_OBJECTS=1 git -C "$l" --no-pager log -1 --format=%T "$orig"); raw_tree=$(env -u GIT_NO_REPLACE_OBJECTS git -C "$l" --no-pager log -1 --format=%T "$orig")
printf '[replace] disabled=%s enabled=%s\n' "$no_replace" "$raw_tree"
test "$no_replace" != "$raw_tree" || fail 'replace-ref probe did not differ'
git -C "$l" config extensions.partialClone origin; git -C "$l" config remote.origin.promisor true
git -C "$l" config remote.origin.partialCloneFilter blob:none
git -C "$l" config remote.origin.url "ext::$a/evil.sh"
git -C "$l" config protocol.ext.allow always
rm -f "$mark"
GIT_NO_LAZY_FETCH=1 git -C "$l" -c core.fsmonitor=false cat-file -e 1111111111111111111111111111111111111111 2>/dev/null || true
test ! -e "$mark" || fail 'lazy fetch/remote helper executed with GIT_NO_LAZY_FETCH'
printf '[partial-clone-config]=present; lazy-fetch-blocked=yes\n'

n="$BASE/nested"; init "$n"; printf outer >"$n/outer"; commit_all "$n" outer
mkdir "$n/inner"; init "$n/inner"; printf inner >"$n/inner/f"; commit_all "$n/inner" inner
nested=$(git -C "$n" -c core.excludesFile=/dev/null ls-files --others --exclude-standard -z | python3 -c 'import sys; print(repr(sys.stdin.buffer.read()))')
printf '[nested-untracked]=%s\n' "$nested"

# Semantic pre/post fence ingredients detect each relevant class of mutation.
c="$BASE/coherence"; init "$c"; printf 'base\n' >"$c/tracked"; printf '*.tmp\n' >"$c/.gitignore"; printf '* -text\n' >"$c/.gitattributes"; commit_all "$c" base
printf 'untracked\n' >"$c/u"; printf ignored >"$c/x.tmp"
hash_file() { python3 -c 'import hashlib,sys; print(hashlib.sha256(open(sys.argv[1], "rb").read()).hexdigest())' "$1"; }
manifest() {
  { git -C "$c" rev-parse --verify 'HEAD^{commit}';
    git -C "$c" -c core.fsmonitor=false ls-files --stage -v -z;
    git -C "$c" -c core.fsmonitor=false -c core.excludesFile=/dev/null ls-files --others --exclude-standard -z;
    printf 'tracked\0u\0x.tmp\0' | git -C "$c" -c core.fsmonitor=false -c core.attributesFile=/dev/null check-attr -z --all --stdin;
    for f in tracked u x.tmp; do test ! -f "$c/$f" || { printf '%s\0' "$f"; hash_file "$c/$f"; }; done; } | python3 -c 'import hashlib,sys; print(hashlib.sha256(sys.stdin.buffer.read()).hexdigest())'
}
assert_manifest_changes() { label=$1; shift; before=$(manifest); "$@"; after=$(manifest); test "$before" != "$after" || fail "coherence fence missed $label"; printf '[coherence:%s] changed\n' "$label"; }
change_head() { printf head >>"$c/tracked"; commit_all "$c" head; }
change_index() { printf index >"$c/indexed"; git -C "$c" add indexed; }
change_ignore() { printf '!x.tmp\n' >>"$c/.gitignore"; }
change_attr() { printf 'tracked text\n' >>"$c/.gitattributes"; }
change_live() { printf live >>"$c/u"; }
assert_manifest_changes HEAD change_head
assert_manifest_changes index change_index
assert_manifest_changes ignore change_ignore
assert_manifest_changes attributes change_attr
assert_manifest_changes live-file change_live

printf '\nPASS\n'
