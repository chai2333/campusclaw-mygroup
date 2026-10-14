# Tasks

此前51项勾选仅代表旧版单助手实现；本任务清单以2026-10-14新版课件为准，重置受影响验收，旧验证记录保留为历史证据。

## 1. Plan and storage
- [x] 1.1 Reconcile existing proposal/design/specs with the current course and authorize update then implementation.
- [x] 1.2 Add idempotent agents/skills/binding schema and additive conversation/message columns with class-safe constraints.
- [x] 1.3 Migrate old settings/conversations into a default agent once, preserve all history and test repeated migration.

## 2. Agent and skill APIs
- [x] 2.1 Implement teacher agent create/update/enable with atomic validated bindings; student enabled-only list/detail and uniform404.
- [x] 2.2 Implement skill file parser, upload/edit/delete/read, name uniqueness and field/tool validation with readable errors.
- [x] 2.3 Add deterministic skill routing, live per-turn snapshots and persisted skill/config metadata.

## 3. Conversation and orchestration
- [x] 3.1 Require enabled agent selection on new private teacher/student conversations; preserve existing disabled-agent conversations and immutable selection.
- [x] 3.2 Apply 2–500 question limits, 24-char first title and latest6 history/500chars with current question exactly once.
- [x] 3.3 Scope keyword/vector/hybrid at source to bound materials, cap4 and apply skill subject filtering before numbering.
- [x] 3.4 Separate system directives from user evidence; fallback role and no-hit continuation with incremental citation stripping.
- [x] 3.5 Implement meta/delta/done/error with pre-stream retrieval errors, post-commit completion, cancellation and locking.

## 4. Frontend and documentation
- [x] 4.1 Build responsive teacher agent/skill management and binding controls with UTF8 counters and upload/edit feedback.
- [x] 4.2 Add agent selection and fixed-agent display to teacher/student chat; render meta skill/citations before text and persist history.
- [x] 4.3 Update SSE parser and meaningful fragmented-UTF8/protocol/error/cancel tests.
- [x] 4.4 Generate OpenAPI from handler annotations in go generate/Docker, retain local Swagger and check all route contracts.

## 5. Acceptance and delivery
- [x] 5.1 Update URL-organized apitest cases and mock provider for multiple agents, bindings, lexical routing, fallback/no-hit and DB parity.
- [x] 5.2 Run independent MySQL/backend suite, frontend typechecks/tests/build, OpenAPI validation and all endpoint scripts; correct genuine failures.
- [x] 5.3 Run real-model isolated math/Chinese acceptance with local credentials, review guidance/skill-off/no-hit/injection results without exposing secrets.
- [x] 5.4 Verify browser teacher/student paths, update README/verification, back up and upgrade local real app preserving business data.
