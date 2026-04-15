# Email Pipeline

**Module**: `nGX/services/email-pipeline`
**SMTP Port**: `:2525` (local dev), `:25` (production)
**Role**: Receives inbound SMTP email and delivers outbound email via direct MX SMTP. Both directions use Kafka as the async work queue.

---

## Outbound Pipeline

### Full Flow: API Call → SMTP Delivery

```
POST /v1/inboxes/{inboxID}/messages/send
  Body: { to, cc, bcc, subject, body_text, body_html, reply_to_id, metadata }
         │
         ▼
services/api/handlers/messages.go
  1. Extract claims from auth middleware (API key scope: inbox:write required)
  2. Parse inboxID from URL path
  3. Decode JSON request body; validate at least one To recipient
  4. Proxy request to inbox service via internal HTTP client
         │
         ▼
services/inbox/handlers/messages.go → service/message_service.go  Send()
  — everything below runs inside a single DB transaction (WithOrgTx) —
  5. Load inbox record → get From address (inbox.Email, inbox.DisplayName)
  6. If reply_to_id is set:
       a. Load parent message → extract its MessageID and References chain
       b. Build In-Reply-To = parent.MessageID
       c. Build References  = parent.References + parent.MessageID  (RFC 5322)
       d. Load the parent's thread → reuse it
     If new conversation:
       a. Build participants list: sender + all To + CC addresses (deduplicated)
       b. INSERT Thread record into DB
  7. Build Message record:
       direction  = outbound
       status     = sending
       From       = inbox address
       To, CC, BCC stored in DB as JSONB arrays (BCC persisted here, not in Kafka)
       Subject, InReplyTo, References, Metadata stored
  8. INSERT Message into DB                            ← message is durable
  9. INCREMENT thread.message_count, update snippet
 10. Publish OutboundJob → Kafka topic email.outbound.queue
       Payload: { message_id, org_id, inbox_id, body_text, body_html, ... }
       NOTE: BCC is intentionally omitted from the Kafka payload — it is read
             back from the DB at delivery time to avoid persisting recipient
             addresses in a durable, multi-consumer log. CC is included because
             it is a visible header and has no privacy concern.
  — transaction commits here —
 11. Return HTTP 201 + message record to caller
       ↑ caller gets a response immediately; delivery is async from this point
         │
         ▼  (asynchronous — email-pipeline service)
services/email-pipeline/outbound/queue.go  QueueConsumer
 12. Kafka consumer reads OutboundJob from email.outbound.queue
 13. Unmarshal job; malformed payload → log error, skip (commit offset)
 14. Load full Message record from DB (WithOrgTx + RLS)
       → this is the authoritative record; BCC is retrieved here
 15. UPDATE message status = 'sending'
       → prevents duplicate sends if job is re-delivered by Kafka
 16. Load Attachment records from DB for this message_id
 17. Resolve body content:
       If message.TextS3Key is set → body text will be fetched from S3
       Otherwise              → use inline BodyText from the Kafka job
       If message.HtmlS3Key is set → body HTML will be fetched from S3
       Otherwise              → use inline BodyHTML from the Kafka job
 18. Build SendJob struct (To, CC, BCC, Subject, S3 keys, inline bodies, headers,
       attachment refs)
         │
         ▼
services/email-pipeline/outbound/sender.go  Sender.Send()
 19. Collect all SMTP RCPT TO recipients: deduplicated union of To + CC + BCC
 20. Fetch body content:
       If TextS3Key → S3.Download() → textBody []byte
       Else         → []byte(job.BodyText)
       If HtmlS3Key → S3.Download() → htmlBody []byte
       Else         → []byte(job.BodyHTML)
 21. For each attachment: S3.Download(att.S3Key) → load binary into memory
       (failed attachment download → log + skip that attachment, don't abort)
 22. Assemble complete RFC 5322 message in-memory (bytes.Buffer):
       a. Address headers:  From, To, CC
                            BCC is NOT written to MIME headers (SMTP envelope only)
                            Reply-To (if set)
       b. Content headers:  Subject, Date, MIME-Version: 1.0
       c. Thread headers:   In-Reply-To, References
       d. Extra headers:    any key/value pairs from msg.Headers JSONB
                            (duplicate thread headers skipped)
       e. Body part:
            text + HTML  → Content-Type: multipart/alternative
                              part 1: text/plain; charset=utf-8; quoted-printable
                              part 2: text/html;  charset=utf-8; quoted-printable
            HTML only    → Content-Type: text/html; charset=utf-8
            text only    → Content-Type: text/plain; charset=utf-8  (default)
       f. If attachments present: wrap body + attachments in multipart/mixed
            each attachment:
              Content-Type: {original content-type}
              Content-Disposition: attachment; filename="..." (or inline + Content-ID)
              Content-Transfer-Encoding: base64  (76-char line wrap per RFC 2045)
       → result: single []byte blob of the complete RFC 5322 message
 23. DKIM-sign the blob (if DKIM signer is configured)
       signing failure is non-fatal → deliver unsigned rather than drop
 24. Deliver via SMTP:
       If SMTP_RELAY_HOST is set (dev/test, e.g. MailHog):
         → smtp.SendMail(relayHost, from, allRcpts, msgBytes)
       Else (production direct delivery):
         → net.LookupMX(first recipient's domain) → sorted by preference
         → Try implicit TLS:  tls.Dial(mxHost:465)
             → smtp.NewClient → MAIL FROM → RCPT TO (×N) → DATA → write blob
         → Fallback STARTTLS: smtp.SendMail(mxHost:25, from, allRcpts, msgBytes)
         │
         ▼
 25. On success:
       UPDATE message SET sent_at = NOW()  (status transitions to sent)
       Publish EventMessageSent → events.fanout
 26. On failure:
       UPDATE message SET status = 'failed'
       Publish EventMessageBounced (BounceCode: "500", reason: error string)
       Return error → Kafka re-delivers the job
```

### Notes

- **Body storage**: outbound body text/HTML travels inline in the Kafka job (`BodyText`/`BodyHTML`). No S3 upload happens at send time. The pipeline uses the Kafka fields directly unless `TextS3Key`/`HtmlS3Key` are set on the DB record (rare; reserved for large bodies).
- **Idempotency**: the `UPDATE status='sending'` at step 15 is the guard against duplicate delivery on Kafka re-delivery.
- **Bounce detection**: the scheduler's `BounceCheck` job marks messages stuck in `'sending'` for 24+ hours as `'failed'` — catches pipeline crashes that never reach step 25/26.

---

## Inbound Pipeline

### Why Two Stages?

| | Single-stage | Two-stage (current) |
|---|---|---|
| SMTP session duration | Parse + S3 + DB time (100ms–2s+) | S3 upload only (~20–50ms) |
| Spike handling | DB pool exhaustion → SMTP backpressure → MTA retries amplify load | Consumer lag absorbs burst; MTAs get fast `250 OK` |
| Processing scale | Tied to SMTP server goroutines | InboundConsumer instances scale independently |
| Failure isolation | DB/S3 failure → SMTP `4xx` (MTA retries) | DB/S3 failure → Kafka re-delivery (no MTA involvement) |

### Full Flow: Receiving MTA → Parsed + Stored Message

```
External MTA connects to go-smtp listener (:2525)
  SMTP handshake: EHLO → MAIL FROM → RCPT TO (×N) → DATA

━━━ STAGE 1: SMTP Fast Path (Enqueuer) ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

services/email-pipeline/inbound/enqueuer.go  Enqueuer.Enqueue()

  1. SPF check (DNS only, no message parsing):
       emailauth.CheckSPF(remoteIP, helo, mailFrom)
       SPFFail     → return SMTP 550 5.7.23 permanent error
                       MTA does NOT retry; message is rejected at the protocol level
       Any other result (Pass, Neutral, SoftFail, None) → continue
       SPF result string is carried forward in the Kafka job payload

  2. io.ReadAll(r) — buffer entire RFC 5322 message bytes in memory

  3. Upload raw bytes to S3:
       Key: inbound/raw/YYYY/MM/DD/{job_id}.eml
       Content-Type: message/rfc822
       (inbox is unknown at this point; date-partitioned flat key)

  4. Build RawEmailJob:
       { job_id, s3_key, from, to[], size_bytes, received_at, remote_ip, helo, spf_result }

  5. Publish RawEmailJob → Kafka topic email.inbound.raw

  6. Return nil → go-smtp sends 250 OK to sending MTA
       ↑ SMTP session is done; all remaining work is async

━━━ STAGE 2: Inbound Consumer (Async) ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

services/email-pipeline/inbound/processor.go  InboundConsumer

  7. Kafka consumer reads RawEmailJob from email.inbound.raw
  8. Unmarshal job; malformed → log error, skip (commit offset, don't block partition)

  9. S3.Download(job.S3Key) → fetch raw .eml bytes back into memory

 10. DKIM verification:
       emailauth.VerifyDKIM(rawData) — verifies signatures against DNS public keys
       Result (Pass / Fail / None) carried forward for DMARC

 11. MIME parse:  pkg/mime.Parse(rawData)
       Extracts: MessageID, InReplyTo, References, Subject
                 From, To, CC, ReplyTo address lists
                 BodyText, BodyHTML
                 Attachment/inline parts (with Filename, ContentType, ContentID, Data)
       Handles: multipart/mixed · multipart/alternative · multipart/related
                Content-Transfer-Encoding: quoted-printable · base64
       On parse error → degrade gracefully; continue with From-only minimal record

 12. DMARC enforcement (per From domain):
       emailauth.CheckDMARC(fromDomain, spfResult, dkimResult)
       → looks up _dmarc.{domain} TXT record; evaluates alignment
       DMARCReject      → discard message silently; commit Kafka offset; return
       DMARCQuarantine  → continue; X-nGX-Spam: true will be set in headers
       DMARCNone / Pass → continue normally

 13. For each recipient address in job.To[]
       (per-recipient errors are logged and skipped; other recipients still processed)

       a. Inbox lookup (no RLS — org is not yet known):
            emailStore.GetInboxByAddress(recipient)
            Not found → log warning, discard this recipient, continue to next

       b. Assign a new UUID (msgID) — each recipient gets its own independent
            message record; one raw .eml can fan out to N inboxes

       c. Build S3 prefix:
            {org_id}/{pod_id}/   if inbox has a pod
            {org_id}/no-pod/     if inbox has no pod
            full prefix: {org_id}/{pod_segment}/{inbox_id}

       d. Upload parsed body and attachments to S3 BEFORE opening the DB transaction:
            Text body   → {prefix}/text/{msgID}.txt        (skipped if empty)
            HTML body   → {prefix}/html/{msgID}.html       (skipped if empty)
            Attachments → {prefix}/attachments/{msgID}/{filename}  (one per part)
              identified by Content-Disposition: attachment  OR  having a Content-ID
            Raw .eml    → already in S3 from Stage 1; key is recorded as-is (not re-copied)

       e. Open DB transaction (WithOrgTx — sets app.current_org_id for PostgreSQL RLS):

            Thread deduplication:
              Collect lookup IDs: [In-Reply-To] + [References...]
              If lookup IDs present:
                emailStore.FindThreadByMessageIDs(orgID, inboxID, ids)
                Found    → attach message to existing thread
                Not found → (fall through to create new thread)
              If no thread found (new conversation or no threading headers):
                isNewThread = true
                Build participants: deduplicated union of From + To + CC headers
                INSERT Thread record

            Merge auth results into headers JSONB (stored with message; no schema change):
              X-nGX-SPF:   {spf_result}     (from Stage 1)
              X-nGX-DKIM:  {dkim_result}    (from Step 10)
              X-nGX-DMARC: {disposition}    (from Step 12, if checked)
              X-nGX-Spam:  true             (only if DMARC quarantine)

            INSERT Message record:
              direction  = inbound
              status     = received
              From, To, CC from parsed MIME headers
              BCC:  not applicable for inbound (BCC recipients never appear in headers)
              TextS3Key, HtmlS3Key, RawS3Key stored — body content stays in S3
              SizeBytes  = len(rawData)
              ReceivedAt = now

            INSERT Attachment records (one row per attachment; S3 key referenced)
              failed attachment S3 upload from step (d) → that attachment skipped

            IncrThreadMessageCount:
              thread.message_count += 1
              thread.snippet        = first 200 chars of body text
              thread.last_message_at = now

            If reply to existing thread:
              MergeThreadParticipants — add any new From/To/CC addresses not already listed

       — transaction commits —

       f. Publish EventMessageReceived → events.fanout  (non-fatal if Kafka unavailable)
       g. If isNewThread: Publish EventThreadCreated → events.fanout

 14. On transient error (DB/S3 unavailable): return error → Kafka re-delivers the job
       Raw .eml is safe in S3; the job is idempotent — re-processing is safe because
       each run generates a new msgID; thread deduplication prevents duplicate threads
```

### S3 Key Scheme

```
inbound/raw/YYYY/MM/DD/{job_id}.eml              ← written by Enqueuer (inbox unknown)
{org_id}/{pod_id}/      {inbox_id}/text/{msg_id}.txt
{org_id}/{pod_id}/      {inbox_id}/html/{msg_id}.html
{org_id}/{pod_id}/      {inbox_id}/attachments/{msg_id}/{filename}
{org_id}/no-pod/        {inbox_id}/...            ← when inbox has no pod
```

### MIME Parsing (`pkg/mime.Parse`)

Handles:
- `multipart/mixed` — body parts mixed with attachments
- `multipart/alternative` — text + HTML variants
- `multipart/related` — HTML with inline images (`cid:`)
- `text/plain`, `text/html` — simple single-part bodies
- `Content-Transfer-Encoding: quoted-printable` — decoded automatically
- `Content-Transfer-Encoding: base64` — decoded with whitespace stripping

Attachments are identified by `Content-Disposition: attachment` or by having a `Content-ID` (inline parts). Inline parts have `IsInline = true` in the parsed output.

### Thread Deduplication

Two emails in the same thread can arrive within milliseconds of each other. Without protection, both would create a new thread. The `FindThreadByMessageIDs` query runs inside `WithOrgTx`, which holds a transaction-level lock, serializing concurrent lookups for the same org.

If `FindThreadByMessageIDs` returns `not found` (or any `IsNotFound` error), the processor creates a new thread. Any other error aborts the transaction and triggers Kafka re-delivery.

---

## Configuration

| Env Var | Default | Description |
|---------|---------|-------------|
| SMTP_LISTEN_ADDR | :2525 | SMTP server listen address |
| SMTP_HOSTNAME | localhost | EHLO hostname |
| SMTP_RELAY_HOST | (empty) | If set, bypass MX lookup and relay to this host:port (e.g. `localhost:1025` for MailHog in dev) |
| DATABASE_URL | postgres://... | Postgres connection |
| KAFKA_BROKERS | localhost:9092 | Kafka brokers for all producers and consumers |
| KAFKA_GROUP_ID | nGX | Base consumer group ID; `-inbound` and `-outbound` suffixes are appended |
| S3_ENDPOINT | http://localhost:9000 | MinIO/S3 endpoint |
| S3_BUCKET | nGX | Object storage bucket |
| S3_ACCESS_KEY_ID | minioadmin | S3 credentials |
| S3_SECRET_ACCESS_KEY | minioadmin | S3 credentials |
