"""A Kit plugin for script/test_dock_overflow.sh.

Its /overflow command asks a confirmation whose title and Markdown message are
longer and wider than the dock: a long title, a long command in a code block,
long paths and links, a wide table, and more lines than fit. It answers the
command once the confirmation is answered.
"""
import json, sys

TITLE = "Deploy the release branch to production, run the database migrations, and restart every worker in all three regions?"
MESSAGE = """This will run the following command on `deploy-01.us-east-1.internal.example.com`:

```sh
kubectl --context production-us-east-1 --namespace payments rollout restart deployment/payments-api deployment/payments-worker deployment/payments-scheduler
```

It touches `/srv/releases/2026-11-04/payments-api/config/production/feature-flags.override.yaml` and https://dashboard.example.com/clusters/production-us-east-1/namespaces/payments/deployments?view=rollout&since=2026-11-04T00:00:00Z

| Region | Cluster | Workers | Expected downtime |
| --- | --- | --- | --- |
| us-east-1 | production-us-east-1-blue | 24 | none (rolling) |
| eu-west-1 | production-eu-west-1-green | 16 | none (rolling) |
| ap-southeast-2 | production-ap-southeast-2-blue | 8 | about 30 seconds |

Migrations to apply:

1. `20261104_add_settlement_batches_and_backfill_from_ledger_entries`
2. `20261104_drop_legacy_payout_columns`

> **Warning:** the second migration can't be rolled back.

Line 1 of a long note.
Line 2 of a long note.
Line 3 of a long note.
Line 4 of a long note.
Line 5 of a long note.
The last line of the message."""


def send(value):
    print(json.dumps(value, separators=(",", ":")), flush=True)


pending = {}
for line in sys.stdin:
    frame = json.loads(line)
    for message in frame if isinstance(frame, list) else [frame]:
        method, identifier = message.get("method"), message.get("id")
        if method == "initialize":
            send({"jsonrpc": "2.0", "id": identifier, "result": {"protocolVersion": 1}})
            send({"jsonrpc": "2.0", "id": "register", "method": "kit/commands/register",
                  "params": {"id": "overflow", "description": "Ask a long confirmation"}})
        elif method == "shutdown":
            send({"jsonrpc": "2.0", "id": identifier, "result": None})
            sys.exit(0)
        elif method == "kit/commands/execute":
            dialog = "confirm:" + str(identifier)
            pending[dialog] = identifier
            send({"jsonrpc": "2.0", "id": dialog, "method": "kit/ui/confirm",
                  "params": {"title": TITLE, "message": MESSAGE, "confirmLabel": "Deploy", "cancelLabel": "Not now"}})
        elif method is None and identifier in pending:
            send({"jsonrpc": "2.0", "id": pending.pop(identifier), "result": None})
