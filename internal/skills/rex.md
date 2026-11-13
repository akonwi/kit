---
name: rex
description: Use Rex, the terminal multiplexer on this machine, to run commands in panes the user can watch and take over, read their output, and be called back when a long job ends. Use when the user mentions Rex, panes, or tabs, or wants a long-running or interactive command they can see.
---

# Rex in Kit

The `rex` command on this machine is:

```sh
REX={{rex}}
```

Run it as `"$REX" …`, since it may not be on `PATH`.

## Start with Rex's guide

Run `"$REX" skill show` and follow it for Rex's commands and options. It comes
from the installed Rex, so it matches that version.

Pass `--autostart=false` to every command. If Rex isn't running, tell the user
instead of starting it.

## Where you run

Your commands run on the Kit server, not in a Rex pane, so parts of Rex's guide
don't apply as written:

- Ignore `REX_SESSION` and `REX_BLOCK` in your environment. They come from
  wherever the Kit server started, not from where the user is.
- You have no pane of your own, so "here" and "beside you" name nothing. Find
  the user's Rex sessions with `"$REX" ls --json`, and ask which to use when
  more than one could fit.
- Open panes with `--focus=false` unless the user asked to see one, so you
  don't move their focus.
- Rex starts a pane's command with the login shell's environment, not yours.
  Pass any variable the command needs with `env`, as below.
- Close the panes you opened once you no longer need them. Don't close or type
  into other panes unless the user asks.

## Be told when a job ends

Your commands see this session's ID in `$KIT_SESSION_ID`. A job can start a
turn in this session when it ends with `kit print --session`, so you can end
your turn instead of waiting:

```sh
"$REX" run --autostart=false --focus=false --label tests -- \
  env KIT_SESSION_ID="$KIT_SESSION_ID" ${KIT_HOME:+KIT_HOME="$KIT_HOME"} sh -c '
    go test ./... > /tmp/tests.log 2>&1
    status=$?
    for _ in $(seq 60); do
      kit print --session "$KIT_SESSION_ID" -- "Rex job tests exited with status $status; its output is in /tmp/tests.log" \
        > /dev/null 2> /tmp/tests.notify && exit
      grep -q "^kit: start prompt:" /tmp/tests.notify || exit
      sleep 10
    done'
```

- `kit print` can't start a turn while this session is busy: a turn is running
  or the user's messages are queued. It then exits with status 1 and
  `kit: start prompt:` on standard error, so the job waits and tries again.
- The message arrives as a prompt from the user, so name the job in it.
- `kit print` waits until the turn it started ends, and interrupting it aborts
  that turn, so leave the pane open until then.
