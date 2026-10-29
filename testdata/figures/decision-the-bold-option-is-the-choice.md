```decision title="Database" status="accepted" date="Mar 12"
- **Postgres** — we already run it
- *Mongo* — no joins we trust
- SQLite — fine until the second writer

Revisit if writes pass 2k a second.
```
