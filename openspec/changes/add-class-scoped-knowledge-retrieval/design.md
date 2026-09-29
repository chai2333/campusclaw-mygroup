# Design

## Context

See `proposal.md` for motivation. The current system is Go `net/http` + MySQL 8.0 with a React/Vite frontend behind Nginx. `knowledge_entries` stores ordered text chunks linked to `materials` by both material ID and class ID. Existing `GET /api/materials?q=...` loads every ready material in the user's class and filters titles/full bodies in application memory; it returns materials rather than ranked source chunks. There is no embedding model, vector index, or vector service in the repository.

## Goals / Non-Goals

**Goals:** add a bounded, deterministic, class-scoped search API that returns individually attributable text chunks; use the existing authenticated user as the sole scope authority; let users navigate from a hit to its protected source material; keep relational material records authoritative.

**Non-Goals:** generated answers, RAG prompts, citations synthesized by a model, file-type expansion, public or cross-class search, and running an embedding model are not part of this change. A vector database is evaluated as an extension, not required for the first delivery.

## Decisions

### Decision 1: Add a dedicated knowledge search endpoint

Add `GET /api/knowledge/search?q=<query>&limit=<optional>` behind the existing authenticated class middleware. Keep `GET /api/materials` and its current list/filter contract for compatibility. The search request does not accept an effective `class_id`, role, or user identity. The service reads the class from the current server-side user and includes it in every candidate query.

Return `{ "results": [...] }`, where each result includes `material_id`, `title`, `original_filename`, `chunk_index`, `snippet`, and a same-origin `source_url` to `/api/materials/{id}`. Do not include absolute paths, storage keys, or class metadata for the source. The source URL is a navigation reference, not a bearer capability; opening it repeats normal authentication and class checks.

### Decision 2: Use MySQL as the first search engine and source of truth

For the first delivery, search `knowledge_entries` joined to `materials` with `materials.class_id = current user class`, `knowledge_entries.class_id = current user class`, and `materials.status = 'ready'`. Use parameterized phrase matching against chunk content and title, with title matches ranked above body matches, then deterministic secondary ordering by material creation time, material ID, and chunk index. Enforce a bounded query length, bounded `limit`, and fixed default limit; reject blank query. This directly supports Chinese substring matching without relying on MySQL's tokenization of CJK text and avoids loading every full material body into Go.

Do not add `FULLTEXT`/NGRAM parser configuration in this change: it has database-version/configuration and token-size implications, while direct bounded substring matching preserves current matching semantics. If corpus size later makes this query expensive, benchmark first and then add an index/search backend without changing the response contract.

### Decision 3: Keep vector storage optional and behind the authorization boundary

The current scale and absence of an embedding provider do not justify making Qdrant or a hosted vector service a mandatory dependency. MySQL remains canonical for class ownership, material status, chunk text, and provenance. If semantic retrieval is added later, a search adapter may store vectors in a separate Qdrant collection keyed by stable chunk identity and carry class ID only as a pre-filter; the API must still recheck every candidate against MySQL before returning it. Vector writes/deletes must be reconcilable from MySQL, and enabled vector-service failures must be surfaced as `503`, not an empty result. No vector service or embedding credentials are added by this change.

Alternative considered: deploy Qdrant plus an embedding API immediately. Rejected because the repository has no model/provider configuration, and adding an unspecified model/service would create an undeployable required dependency and new secret/latency/cost concerns. Alternative considered: keep the current in-memory material scan. Rejected because it cannot give chunk-level provenance and scales with total corpus text loaded per request.

### Decision 4: Revalidate provenance before serializing results

The SQL query itself joins ready materials using both class constraints. Result mapping only emits allowed provenance fields. A missing or inconsistent parent material excludes the chunk. The client opens `source_url` through the existing protected detail flow; it never receives a file-system path or a direct unguarded upload URL. Existing detail and download authorization remain unchanged.

### Decision 5: Integrate search results in the materials experience

Keep the current toolbar search control, but submit it to the dedicated search endpoint after debounce and render chunk snippets as result cards with source title, file name, and segment position. Selecting a result opens the existing material detail view. Empty query continues to show the normal material list; a valid query with no results shows a separate no-matches state. Search error states retain the current identity and distinguish `401`, `403`, `404`, and `503` under existing frontend behavior.

## Risks / Trade-offs

- `[Substring search can become slow as the class corpus grows]` → Bound query length and result count, constrain by class/status in SQL, avoid loading full documents, and add a representative corpus benchmark; revisit a full-text or vector backend only with measurements.
- `[Substring phrase matching does not find semantic paraphrases]` → State keyword semantics in the UI/API and retain the optional vector adapter boundary for a separately specified semantic-search iteration.
- `[Chunk boundaries may split a phrase or citation context]` → Return the original chunk and its stable sequence number; show enough of the stored chunk for verification and link to full protected material details.
- `[Relational records and a future vector index could drift]` → MySQL remains authoritative; any future vector integration must be rebuildable and reconcile candidates against MySQL before responses.

## Migration Plan

No existing data migration is required for the first delivery: all indexed chunks already exist in `knowledge_entries`. Deploy the new API and frontend together through the existing web/api/db Compose path. Rollback by reverting the application image; existing material rows, chunks, and endpoints remain compatible. If a later vector adapter is introduced, it must be optional, backfillable from `knowledge_entries`, and removable without affecting MySQL records or material access.
