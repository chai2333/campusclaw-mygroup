# Verification Evidence

Date: 2026-09-30

Automated suites:

- `go test ./... -p 1` against an isolated real MySQL 8 database (`campusclaw_test`): passed.
- `npm run build`: passed.
- `npm test`: 12/12 passed.
- `docker compose up --build -d`: API, MySQL, Qdrant and Web started; API health became healthy.

Runtime observations use `Authorization: Bearer` through `http://localhost:8080`. Secrets and tokens were not written to this file.

| Scenario | Evidence |
| --- | --- |
| 原文与切片分别存在 | `internal/db/db_test.go`; real MySQL inspection showed `knowledge_chunks` plus preserved seed material/body. |
| 预处理不改写原文 | `internal/knowledge/chunks_test.go`; indexer reads but never updates `knowledge_entries.body_text`. |
| 预处理后的字符位置 | `internal/knowledge/chunks_test.go` Unicode/preprocessing range cases. |
| 自动窗口切分 | `internal/knowledge/chunks_test.go`. |
| 自动策略忽略自定义参数 | `internal/knowledge/chunks_test.go`. |
| 自定义切分参数有效 | `internal/knowledge/chunks_test.go`. |
| 自定义切分参数越界 | `internal/knowledge/chunks_test.go`; rebuild validates before replacement. |
| Markdown 标题层级切分 | `internal/knowledge/chunks_test.go`; runtime rebuilt `seed-a-intro` into two ready hierarchy chunks. |
| 两库通过切片 ID 关联 | Runtime MySQL IDs 6/7 equalled Qdrant point/payload `chunk_id` 6/7. |
| 向量记录不含正文 | `internal/retrieval/clients_test.go`; runtime payload inspection contained only five identifiers. |
| 嵌入失败保留正文 | `TestIndexerEmbedFailureMarksFailedKeepsMaterial`. |
| 使用新策略重建 | Teacher hierarchy rebuild returned 200; old point 1 was replaced by points 6/7. |
| 未重建时保留旧索引 | Handler validates strategy before `IndexMaterial`; invalid custom cases are covered by chunk tests. |
| 学生尝试重建 | Runtime student rebuild returned 403. |
| 重建过程中嵌入失败 | `TestIndexerEmbedFailureMarksFailedKeepsMaterial`; stale points fail MySQL ready/class recheck. |
| 关键词检索 | `TestKeywordClassScopeAndReadyOnly` against real MySQL. |
| 向量检索 | `TestVectorThresholdAndClassRecheck`. |
| 默认混合检索 | `TestRetrieveRejectsBlankQueryAndBadMode`; runtime omitted client authority and returned mode `hybrid`. |
| RRF 融合排序 | `TestHybridRRFFusionAndCap`; implementation uses RRF k=60 after both path filters. |
| 空白查询 | Runtime returned 400. |
| 无相关候选 | Retrieval tests and runtime A-class keyword query for B-only `实践` returned 200 with zero hits. |
| 教师和学生检索本班材料 | Shared `auth.Class` middleware plus class-scoped service tests. |
| 伪造其他班级编号 | Runtime body included `class_id:999`/`role:teacher`; results remained A-class. |
| 跨班关键词表现为无命中 | Runtime A-class query for B-only `实践`: 200, empty hits. |
| 向量候选回表再次隔离 | `TestVectorThresholdAndClassRecheck`. |
| 用户没有班级归属 | `auth.Class` returns `class_membership_required` before handlers. |
| 命中包含完整来源 | Browser acceptance displayed title, chunk index, range, excerpt and source action. |
| 摘录来源是 MySQL | `fetchByIDs` reads `chunk_text` from MySQL; Qdrant search requests `with_payload:false`. |
| 同一材料有多个命中 | Runtime hierarchy rebuild produced two traceable chunks for the same material; UI rendered ranked hits/citations. |
| 来源材料不可用 | Source action calls protected `/api/materials/{id}` with existing same-shape 404 contract. |
| Qdrant 停止时关键词仍可用 | `TestVectorModeDependencyFailure`; runtime stopped Qdrant and the Web keyword query still returned the A-class hit. |
| Qdrant 停止时向量模式失败 | `TestVectorModeDependencyFailure`; with Qdrant stopped the Web vector query displayed the 503 dependency message, then Qdrant was restored. |
| 嵌入服务不可用 | `TestVectorModeDependencyFailure`; hybrid/vector fail, keyword remains usable. |
| 有依据时生成简短回答 | `TestGroundedAnswerSendsOnlyEvidenceAndQuestion`; runtime `/api/ask` returned cited answer. |
| 客户端 system 消息无效 | Ask request DTO decodes only `question`; runtime supplied `system` was ignored. |
| 无依据时不调用模型 | `TestNoEvidenceShortCircuitsGateway` asserts zero gateway calls and fixed answer. |
| 对话网关失败 | `TestRetrievalAndDialogueFailuresPropagate`; HTTP maps failure to 503. |
| 回答编号对应引用数组 | `TestInvalidModelCitationIsRejected`; only ordinals present in the validated answer become citation cards. |
| 引用可以打开原材料 | Browser acceptance showed `查看材料`; it uses protected material detail API. |
| 客户端不能直连向量数据库 | Compose exposes no Qdrant host port; only Web publishes 8080. |
| 服务凭据保持在服务端 | Compose injects model/Qdrant settings only into API; frontend build contains no configuration secrets. |
| 日志和缓存保护正文 | User responses use `Cache-Control: no-store`; runtime header verified; code/log scan recorded below. |

Security scan commands:

```text
compare configured secret values of length >= 8 against frontend/dist
rg -n "slog\.|log\." backend/internal backend/cmd
```

The minified frontend contains the literal word `Authorization` because it constructs the required Bearer request header. No configured secret value of length at least eight matched the bundle; shorter development passwords are excluded from substring scanning because values such as `tea` occur naturally inside JavaScript words. Server logs contain startup/failure categories only and do not log question text, chunk text, vectors, Authorization values or gateway keys.
