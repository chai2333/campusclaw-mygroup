# Proposal

## Why

当前版本仅能在本班材料标题和完整正文中做普通子串筛选，无法处理语义相近但表述不同的问题，也没有稳定的切片位置、相关性排序和引用编号。第四课需要在第 3 课认证、班级隔离和材料入库基础上，建立可追溯的混合检索与有依据问答，使每个结论都能回到本班原始材料核验。

## What Changes

- 将知识库正文与检索切片分离：MySQL 保存上传原文、切片正文、序号、字符区间和索引状态，原文不因切分或预处理被改写。
- 支持 `auto`、`custom`、`hierarchy` 三种切分策略；默认窗口约 800 字、重叠 80 字，并允许教师按新策略重建材料索引。
- 接入服务端嵌入网关与 Compose 内部 Qdrant。Qdrant collection 为 `campusclaw_chunks`、使用余弦距离，只保存向量和关联标识，向量主键与 MySQL 切片 ID 一致。
- 提供 `keyword`、`vector`、`hybrid` 三种检索模式并默认使用 `hybrid`：关键字路径使用 MySQL ngram 全文索引，向量路径使用班级过滤与 0.35 相似度阈值，混合路径以 RRF `k = 60` 融合两路名次。
- 所有检索范围只取当前登录会话的 `class_id`；客户端携带的班级声明无效，MySQL 查询和 Qdrant 查询均过滤班级，向量候选回表时再次校验。
- 每条命中返回材料标题、材料 ID、切片 ID、切片序号、字符区间、MySQL 原文摘录及受保护材料引用；支持从命中打开原材料。
- **BREAKING**：嵌入失败不再回滚已成功保存的材料和正文；对应切片保留并标记 `failed`，不写入不完整向量，只有 `ready` 切片可被检索。
- 新增 `POST /api/ask`：固定以本班混合检索取前 4 条依据，有命中才调用对话网关生成带 `[1]`、`[2]` 引用的简短回答；无命中直接返回“资料中未找到相关内容”，且不调用生成模型。
- Qdrant、嵌入网关和对话网关仅供服务端访问；浏览器仍只访问本站 Go API，服务凭据不下发客户端。

## Non-goals

- 不实现第 5 课的流式长对话、对话历史或多轮记忆。
- 不实现交叉编码器重排序、自动语义切分、父子切片检索或跨班级知识共享。
- 不把切片正文复制进 Qdrant，不向宿主机或浏览器公开 Qdrant 端口。
- 不改变第 3 课材料详情与下载的同形 404 授权合同。

## Capabilities

### New Capabilities

- `knowledge-retrieval`: 覆盖知识正文切分与重建、关键词/向量/混合检索、班级隔离、来源溯源以及只依据本班命中片段生成带引用的简短回答。

### Modified Capabilities

无；当前主规格目录没有已归档能力规格。

## Impact

- 数据模型：扩展知识正文记录并新增可索引切片、切分参数、字符区间和索引状态；为中文关键字检索增加 ngram 全文索引。
- 外部依赖：Compose 增加不发布宿主端口的 Qdrant；API 增加服务端嵌入与对话网关配置。
- API：增加三模式知识检索、材料索引重建和 `POST /api/ask`，扩展上传参数及命中/引用 DTO。
- 后端：新增切分、嵌入、向量索引、RRF 融合、问答编排与两库一致性处理。
- Web：增加检索模式、切分策略、索引状态、命中来源和回答引用交互。
- 迁移：已有材料按 `auto` 策略补齐切片和向量；MySQL 始终为正文、归属和来源的权威数据源。

## Course References

- [第 4 课总览](https://devops.hello1023.com/课件/第4课-课件-可追溯知识库检索/index.html)
- [目标与范围](https://devops.hello1023.com/课件/第4课-课件-可追溯知识库检索/pages/scope.html)
- [RAG 与技术选型](https://devops.hello1023.com/课件/第4课-课件-可追溯知识库检索/pages/rag.html)
- [检索链路](https://devops.hello1023.com/课件/第4课-课件-可追溯知识库检索/pages/flow.html)
- [切分策略](https://devops.hello1023.com/课件/第4课-课件-可追溯知识库检索/pages/chunk.html)
- [班级隔离](https://devops.hello1023.com/课件/第4课-课件-可追溯知识库检索/pages/class-scope.html)
- [回答与引用](https://devops.hello1023.com/课件/第4课-课件-可追溯知识库检索/pages/answer.html)
