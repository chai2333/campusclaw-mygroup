# Tasks

## 1. Protocol and storage
- [x] 1.1 Implement same-repo MCP server and Compose wiring; verify Bearer, JSON-RPC errors, health, two real tools and direct protocol tests.
- [x] 1.2 Add additive MCP registry/cache/change/audit storage and tool-message snapshots; verify idempotent migration and preserved old history.
- [x] 1.3 Implement encrypted credentials, safe URL configuration, handshake/discovery/replacement/disconnect/CRUD; verify no token response and class isolation.

## 2. Binding and execution
- [x] 2.1 Extend skill references and transactional save validation; verify malformed, foreign, disconnected and missing tools rejected.
- [x] 2.2 Implement discovered native tool declarations, structured model decisions and parameter Schema validation; verify multi-round success and invalid arguments.
- [x] 2.3 Implement current authorization, local failures, result injection checks, quotas and graceful final generation; verify denied/error/ok, deletion and quota termination.
- [x] 2.4 Implement audit and discovery-change approval; verify full descriptions/schemas, changed tools unavailable before approval and teacher-only scoped history.

## 3. UI and contracts
- [x] 3.1 Build responsive teacher MCP management with no token echo; verify connect/discover/approve/disconnect and audit in browser.
- [x] 3.2 Add cumulative pre-text meta tool calls and persisted chat display; verify ordering, multiple rounds and reload, frontend protocol tests.
- [x] 3.3 Extend annotated OpenAPI and local Swagger; verify generation, OAS validation and response contracts.

## 4. Acceptance and delivery
- [x] 4.1 Extend URL-organized API scripts and run_all HTML reporting; verify real same-Compose MCP registration-to-audit path plus failure/authorization cases.
- [x] 4.2 Run complete backend/MySQL suite, frontend typechecks/tests/build and API regressions; record all outcomes accurately.
- [x] 4.3 Run real-model MCP acceptance and injection/two-step/limit samples; record deterministic versus model behavior and classroom peer limitation.
- [x] 4.4 Back up and upgrade local main service preserving business data; verify health and browser teacher/student paths, update README and verification.
- [x] 4.5 Check secrets/diff/OpenSpec and publish validated changes to GitHub; verify matching remote tree and give commit link.
