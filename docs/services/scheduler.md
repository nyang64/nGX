# Scheduler

**Module**: `nGX/services/scheduler`
**Role**: Background maintenance jobs for data hygiene.

## Jobs

### bounce_check

Marks messages stuck in `sending` state for over 24 hours as `failed`. This catches cases where the email-pipeline crashed or was killed after inserting the message record but before updating its status to `sent` or `failed`.

```sql
UPDATE messages
SET    status     = 'failed',
       updated_at = NOW()
WHERE  status     = 'sending'
  AND  created_at < NOW() - INTERVAL '24 hours'
```

Logs the count of affected rows at `INFO` level if any were updated.

### draft_expiry

Rejects pending drafts that have passed their `scheduled_at` deadline. The `scheduled_at` column represents the intended send time; if a draft has not been approved before that time, it can no longer be sent in time and is automatically rejected.

```sql
UPDATE drafts
SET    review_status = 'rejected',
       review_note   = 'expired: scheduled send time has passed',
       updated_at    = NOW()
WHERE  review_status = 'pending'
  AND  scheduled_at IS NOT NULL
  AND  scheduled_at < NOW()
```

Drafts with no `scheduled_at` are not affected.

## Design Notes

**No distributed locking needed**: both jobs are idempotent SQL `UPDATE` statements with precise `WHERE` clauses. Running the same job twice (e.g. during a rolling deploy with two scheduler instances briefly overlapping) is safe — the second run will find zero rows matching the condition and be a no-op.

**No Kafka events published**: the scheduler writes directly to PostgreSQL without publishing domain events. These are maintenance operations, not user-initiated actions, so downstream systems (event-dispatcher, webhooks, WebSocket) are not notified. If event emission becomes necessary, the jobs can be extended to also publish to `events.fanout` after the UPDATE.

**Direct pool access**: both jobs bypass RLS by using the raw `pgxpool.Pool` directly, not `db.WithOrgTx`. This is intentional — the jobs operate across all orgs and cannot set a per-org RLS context. The queries include no org filter, relying on the scheduler not being exposed to external clients.

## Configuration

| Env Var | Default | Description |
|---------|---------|-------------|
| DATABASE_URL | postgres://... | Postgres connection |
| LOG_LEVEL | info | Logging verbosity |
