# Design

## Context

See `proposal.md` for motivation. The current Go/MySQL/React system already authenticates users, derives class membership server-side, stores uploaded `.txt/.md` material and writes ordered text into `knowledge_entries`. Existing search loads complete ready materials and performs application-memory substring filtering. There is no stable retrieval chunk identity, character range, embedding gateway, vector index, answer gateway or citation contract.

The fourth-lesson architecture keeps the browser behind Nginx and Go. MySQL remains authoritative for original text, class ownership and excerpts; Qdrant is an internal derived index. This change crosses upload, storage, search, authorization, Compose and Web behavior.

## Goals / Non-Goals

**Goals:** implement all fourth-lesson behavior: three chunking strategies, keyword/vector/hybrid retrieval, Qdrant storage, dual class filtering, traceable excerpts, index rebuild, no-evidence handling and short cited answers.

**Non-Goals:** streaming or multi-turn chat, rerankers, semantic-boundary chunking, parent-child retrieval and cross-class search remain outside this change.

## Decisions

### Decision 1: MySQL is authoritative; Qdrant is a rebuildable vector index

`knowledge_entries` retains one original `body_text` per material. Add `knowledge_chunks` with a stable ID, `knowledge_entry_id`, `material_id`, `class_id`, `chunk_index`, `chunk_text`, `char_start`, `char_end`, strategy metadata and `index_status` (`pending`, `ready`, `failed`). Character offsets use one documented character-count convention and are end-exclusive. Only ready chunks are searchable.

MySQL stores original body, chunk text, ranges, class ownership and status. Qdrant collection `campusclaw_chunks` uses cosine distance and the embedding provider's configured dimension. Its point ID equals `knowledge_chunks.id`; payload contains only `class_id`, `material_id`, `knowledge_entry_id`, `chunk_id` and `chunk_index`. It never stores `chunk_text`. Retrieval always returns to MySQL for text and authorization.

Alternative: store text in Qdrant payload to avoid a lookup. Rejected because it duplicates sensitive class content and complicates consistency. Alternative: use Qdrant IDs unrelated to chunk IDs. Rejected because it adds a mapping lookup with no course benefit.

### Decision 2: Implement the three course chunking strategies exactly

All strategies accept `body_text` and return ordered chunks with text and positions in the text actually split.

- `auto` is the default: maximum 800 characters, overlap 80 characters, preferring blank lines, then newlines, then Chinese full stops before a forced boundary. Any custom length, overlap or preprocessing parameters supplied with `auto` are ignored.
- `custom` accepts maximum length 100–2000 characters, overlap 0%–50%, and a separator choice of newline, blank line or full stop. It may remove URLs and email addresses or collapse consecutive whitespace before splitting. These transforms affect only the indexing input.
- `hierarchy` splits Markdown by `#`, `##` and `###` headings, preserves each heading in its section, and applies the auto window rules to an oversized section.

The uploaded file and `knowledge_entries.body_text` remain byte-for-byte/character-for-character as accepted at upload. When custom preprocessing changes text, offsets refer to the preprocessed indexing text and are not presented as raw-file offsets. Seed material and any request without an explicit strategy use `auto`.

Alternative: sentence or semantic-boundary splitting. Rejected because it is outside the fourth-lesson contract and would make chunk sizes and embedding costs less predictable.

### Decision 3: Indexing is an explicit post-storage pipeline

After material and original body are safely stored, the retrieval module splits the indexing input, inserts pending MySQL chunks, requests embeddings on the server, upserts one Qdrant point per chunk, then marks completed chunks ready. Embedding credentials and vector dimensions are validated at startup and never sent to the browser.

If embedding fails, the material and original body stay available; affected MySQL chunks become failed and no incomplete vector point is retained. A teacher can rebuild one material with new strategy parameters. Rebuild first removes old Qdrant points and old chunks, then recreates both indexes; a failure leaves the new chunks failed rather than silently reusing stale vectors. Seed startup backfills missing indexes with `auto` without duplicating ready chunks.

MySQL and Qdrant cannot share a transaction. The service therefore records states in MySQL, performs idempotent point upserts/deletes, and reconciles orphan/missing points from authoritative chunks at startup or explicit repair. A point is never returned until its MySQL chunk is ready.

### Decision 4: Provide keyword, vector and hybrid retrieval modes

The retrieval request contains a nonblank natural-language query, an optional bounded result limit and `mode` in `keyword`, `vector`, `hybrid`; omitted mode means `hybrid`.

- `keyword` queries a MySQL `FULLTEXT` index configured with the ngram parser and token size 2. It applies current-session `class_id` and ready status in SQL, orders by full-text relevance and does not call either gateway or Qdrant.
- `vector` sends the question to the embedding gateway, searches Qdrant using cosine distance and a payload filter for the session class, drops candidates below 0.35, then fetches ready chunks from MySQL with the same class constraint.
- `hybrid` executes both paths, applies each path's absolute filter first, then combines ranks using Reciprocal Rank Fusion with `k = 60`. A chunk absent from one path receives no contribution from that path. Stable IDs break equal-score ties.

Each hit includes chunk/material identifiers, material title, chunk index, character interval, MySQL excerpt, protected material reference, per-path scores/ranks when applicable, and final rank. Retrieval diagnostics distinguish whether a query vector was created; keyword mode produces none. Full-text scores and cosine similarity are never directly added.

An empty query returns 400. A valid query without retained candidates returns 200 and `hits: []`. Qdrant or embedding failure leaves keyword mode operational, while vector and hybrid return 503. Results are `no-store`.

### Decision 5: Enforce class scope on both retrieval paths and on return-to-MySQL

The API reads `class_id` only from the current authenticated user context. It discards any class or role supplied in query strings, headers or JSON. Keyword SQL includes session class and ready state. Qdrant uses the same class as a payload filter, and every vector candidate is queried back from MySQL with that class and ready state before serialization.

A term that exists only in another class produces the same 200 empty result as any no-match query; it does not return 403/404 or a cross-class count. Opening a cited material still uses the existing detail authorization, where cross-class and missing IDs are both 404. Qdrant is reachable only on the Compose internal network and publishes no host port.

### Decision 6: Generate answers only from retrieved evidence

`POST /api/ask` accepts the user's current question and discards client-provided system instructions. It runs the same class-scoped hybrid retrieval with a fixed top 4. If no chunk survives filtering, the API returns 200 with answer `资料中未找到相关内容`, empty `hits`/`citations`, and does not call the dialogue gateway.

When evidence exists, the server builds its own system instruction and sends only the user question plus numbered material titles, chunk indices and chunk text to the dialogue gateway. The answer is short and cites evidence as `[1]`, `[2]`, matching the response citation order. Each citation repeats traceable hit metadata and opens the protected material. The model never receives vector components or other-class content. Dialogue-gateway failure returns 503 rather than an uncited fallback answer.

### Decision 7: Extend the existing Web experience

The materials page gains a knowledge-search surface with mode selection and hybrid as default. Hits show excerpt, material title, chunk number and range, per-path ranking information appropriate to the selected mode, and an action to open the source material. A separate ask surface shows a concise answer and ordered citation cards; the no-evidence fixed response is visually distinct.

Teacher upload/rebuild controls expose the three strategies and only enable custom parameters for `custom`. Indexing status is visible without exposing service credentials or internal endpoints. Existing identity recovery, error handling, responsive behavior and keyboard accessibility continue to apply.

### Decision 8: Compose and configuration keep dependencies server-side

Compose adds Qdrant with a persistent named volume, internal healthcheck and no published host port. API configuration adds Qdrant internal URL/collection, embedding base URL/model/dimension/key and dialogue base URL/model/key. Only the API receives these values. Nginx proxies only application APIs; it never proxies Qdrant or the external gateways.

`/ready` checks required configuration, MySQL, upload storage and Qdrant collection compatibility. A Qdrant runtime outage does not make keyword requests fail, but readiness reports the degraded dependency. Logs contain request mode, latency, candidate counts and failure category without question text, chunk text, credentials or vectors.

## Risks / Trade-offs

- `[MySQL ngram parser availability varies by image/build]` → Pin and verify a MySQL image with the parser, create the index in migration, and fail migration clearly if unsupported.
- `[Embedding model or dimension changes invalidate vectors]` → Store model/dimension metadata, reject mismatched collections, and require a rebuild before vector/hybrid readiness.
- `[MySQL and Qdrant can drift]` → Use MySQL states, idempotent point IDs and reconciliation; reauthorize every candidate on return.
- `[Preprocessing makes offsets differ from raw files]` → Label offsets as indexing-text positions and always provide the actual MySQL excerpt plus source material link.
- `[Hybrid retrieval latency equals its slower path]` → Execute independent paths concurrently with deadlines, bound candidate counts and return 503 when a mandatory hybrid dependency fails.
- `[Weak candidates can cause unsupported answers]` → Apply path thresholds before RRF, never pad results, and skip the dialogue gateway when no evidence remains.

## Migration Plan

1. Add body/chunk/index-state schema and the ngram full-text index without deleting current material or files.
2. Deploy Qdrant internally and create `campusclaw_chunks` with the configured dimension and cosine distance.
3. Convert existing ordered `knowledge_entries` content into one original `body_text` per material, preserving reconstructable text and ownership.
4. Backfill existing and seed materials through `auto`; verify point ID, chunk ID, class, count and excerpts on both stores.
5. Enable retrieval and ask endpoints only after schema and collection compatibility checks pass, then deploy the Web controls.
6. Roll back application binaries without deleting MySQL text or material files. Vector features remain disabled if Qdrant is unavailable; backfilled chunks and points may be retained for a later retry.

Rollback never uses `docker compose down -v`; destructive removal of Qdrant or MySQL volumes is outside deployment rollback.
