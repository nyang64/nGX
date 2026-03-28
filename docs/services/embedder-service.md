# Embedder Service

**Module**: `agentmail/services/embedder`
**Port**: none (Kafka consumer only)
**Role**: Asynchronously generates semantic embeddings for email messages and stores them in PostgreSQL for use by the search service.

## Responsibilities

- Consume `message.received` and `message.sent` events from `events.fanout`
- Fetch the plain-text body of each message from S3
- Send the body to a local embedding server (OpenAI-compatible API)
- Truncate the returned vector to 256 dimensions (MRL)
- Write the embedding to `messages.embedding` and stamp `messages.embedded_at`

## Why Async

Embedding generation takes 10–300 ms depending on the model and hardware. Running it inline on the inbound path would add unacceptable latency to SMTP processing. By consuming Kafka events asynchronously:

- Inbound message delivery is unaffected by embedding latency or model downtime
- Bursts (e.g. bulk imports) are absorbed by consumer lag rather than backpressure
- Historical messages can be backfilled by replaying Kafka offsets or running a batch job scanning for `embedded_at IS NULL`

## Data Flow

```
email.inbound.raw → InboundConsumer → Postgres + S3
                                    → events.fanout (message.received)
                                            |
                                            v
                                    Embedder Consumer
                                            |
                                    1. DB: get body_text_key
                                    2. S3: download body text
                                    3. HTTP: POST /embeddings
                                    4. DB: UPDATE messages SET
                                           embedding = $vec::vector,
                                           embedded_at = NOW()
```

## Embedding Model

The service calls any OpenAI-compatible embedding server. The recommended setup is `nomic-embed-text-v1.5` served by [Infinity](https://github.com/michaelfeil/infinity):

In local development, Infinity is included in `docker-compose.yml` and starts automatically — no separate setup needed. The model is cached in the `infinity_cache` Docker volume after the first download.

For production or GPU-accelerated setups, run it directly:

```bash
docker run -p 7997:7997 \
  michaelf34/infinity:latest \
  v2 --model-id nomic-ai/nomic-embed-text-v1.5 \
  --port 7997
```

Infinity's embedding endpoint is at `POST /embeddings` (no `/v1/` prefix). The `pkg/embedder` client calls `/embeddings` directly. Infinity supports request batching — concurrent calls are batched into a single forward pass, giving significant throughput gains on GPU.

For Ollama, set `EMBEDDER_URL=http://localhost:11434/v1` so the client appends `/embeddings` to get the correct path:

```bash
ollama pull nomic-embed-text
export EMBEDDER_URL=http://localhost:11434/v1
```

### MRL Truncation

`nomic-embed-text-v1.5` is trained with Matryoshka Representation Learning (MRL). The first N dimensions carry the most information. The embedder client keeps only the first 256 of the 768 returned dimensions:

| Dims | Recall vs. full | HNSW index size (1M messages) |
|------|----------------|-------------------------------|
| 768 | 100% | ~3 GB |
| 256 | ~97% | ~1 GB |
| 64 | ~88% | ~250 MB |

256 was chosen as a good trade-off between quality and memory footprint.

## Shared Client (`pkg/embedder`)

The embedding HTTP client and the `VectorLiteral` formatter are in `pkg/embedder` so both the embedder service and the search service can use the same code:

```go
client := embedder.New("http://infinity:7997", "nomic-embed-text-v1.5", 256)

vec, err := client.Embed(ctx, "invoice payment overdue")
// vec: []float32 of length 256

// Format for use as a Postgres parameter:
lit := embedder.VectorLiteral(vec)  // "[0.123,0.456,...]"
// Use as: $1::vector in SQL
```

## Error Handling

All errors in the consumer are non-fatal:

| Failure | Behaviour |
|---------|-----------|
| DB lookup fails | Log error, commit offset, skip message |
| S3 download fails | Log error, commit offset, skip message |
| Empty body text | Skip silently (no embedding possible) |
| Embedding server error | Log error, commit offset, skip message |
| DB write fails | Log error, commit offset, skip message |

Skipping rather than retrying ensures a transient embedding server outage does not stall the Kafka partition. Missed messages are recoverable via `embedded_at IS NULL` scanning.

## Configuration

| Env Var | Default | Description |
|---------|---------|-------------|
| `DATABASE_URL` | `postgres://...` | PostgreSQL connection string |
| `KAFKA_BROKERS` | `localhost:9092` | Comma-separated Kafka broker list |
| `KAFKA_GROUP_ID` | `agentmail` | Consumer group prefix (`-embedder` appended) |
| `S3_ENDPOINT` | `http://localhost:9000` | S3 / MinIO endpoint |
| `S3_BUCKET` | `agentmail` | Bucket containing message bodies |
| `S3_ACCESS_KEY_ID` | `minioadmin` | S3 access key |
| `S3_SECRET_ACCESS_KEY` | `minioadmin` | S3 secret key |
| `S3_USE_PATH_STYLE` | `true` | Set `true` for MinIO; `false` for AWS S3 |
| `EMBEDDER_URL` | `http://localhost:7997` | Base URL of the OpenAI-compatible embedding server |
| `EMBEDDER_MODEL` | `nomic-embed-text-v1.5` | Model name sent in the embeddings request |
| `LOG_LEVEL` | `info` | Log verbosity |
| `LOG_FORMAT` | `json` | Log format (`json` \| `text`) |
