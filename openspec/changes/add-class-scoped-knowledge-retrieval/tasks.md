# Tasks

## 1. Search API and query contract

- [ ] 1.1 Define bounded query and result DTOs for `GET /api/knowledge/search`; verify blank, overlong, and out-of-range inputs have explicit API tests.
- [ ] 1.2 Implement parameterized MySQL chunk search joined to ready materials with both class constraints and stable ranking; verify matching rows are class-filtered, bounded, and deterministically ordered.
- [ ] 1.3 Register the endpoint behind current-session class authorization; verify unauthenticated users are rejected, unassigned users receive 403, and forged class/role inputs cannot change results.
- [ ] 1.4 Map each hit to snippet, material ID/title/original filename/chunk index/source URL only; verify internal storage keys and filesystem paths never appear in JSON.
- [ ] 1.5 Add tests for no matches, repeated hits from one source, deleted/not-ready source records, database failure, and `Cache-Control: no-store`; verify contract and status codes.

## 2. Web search and provenance navigation

- [ ] 2.1 Add a typed frontend search API client using the authenticated same-origin endpoint; verify request parameters are encoded and no client-supplied class scope is sent.
- [ ] 2.2 Render chunk-level hits with source title, filename, snippet, and segment position while preserving the empty-query materials list; verify matches and no-matches states are distinct.
- [ ] 2.3 Connect a hit to the existing protected material detail flow; verify it displays the full source and handles 401/403/404/503 without losing valid session state on 503.
- [ ] 2.4 Verify the search experience by browser or component tests for debounce, keyboard/focus behavior, small-screen layout, and source navigation.

## 3. Security, performance, and delivery validation

- [ ] 3.1 Run a two-class negative test matrix using identical matching content; verify each role sees only its current class even when request fields claim the other class.
- [ ] 3.2 Run the backend test suite and frontend type/build checks; verify the new route, existing material list/detail/download behavior, and Compose build all pass together.
- [ ] 3.3 Exercise representative larger class data and document query latency/result bounds; verify the request does not load whole corpus documents into application memory.
- [ ] 3.4 Run `openspec validate add-class-scoped-knowledge-retrieval --strict`; verify all new scenarios and planning artifacts pass strict validation before implementation begins.
