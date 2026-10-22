# CampusClaw

一个按班级隔离的课程材料知识库和学习助手。教师上传 UTF-8 的 `.txt` 或 `.md` 材料，创建多个本班智能体、编写技能文件并绑定材料；教师和学生可选择智能体，在自己的会话中连续追问，并收到带资料出处的流式回答。第6课支持登记本班MCP服务器，让模型在技能绑定的工具中选择并调用，教师可查看逐轮审计。

技术栈：Go `net/http` + MySQL 8.0 + React 18/TypeScript/Vite + Nginx。

## 快速启动

```bash
cp .env.example .env
# 填写 .env 中的全部空秘密值
docker compose up --build -d
```

服务监听 `http://localhost:${WEB_PORT:-8080}`。

初始化提供“默认学习助手”“数学学习助手”“语文阅读助手”三个独立应用；数学/语文应用分别绑定相应内置技能，资料范围由教师另行设置。再次启动保留教师修改及已移除的绑定。

预置账号：`a-teacher`、`a-student`、`b-teacher`、`b-student`。密码来自 `.env` 中的 `SEED_TEACHER_PASSWORD` 和 `SEED_STUDENT_PASSWORD`。

## 配置

| 变量 | 说明 |
| --- | --- |
| `APP_ENV` | `development`（HTTP 不加 Secure）、`production` 或 `test` |
| `SESSION_SECRET` | 会话 HMAC 密钥，必填 |
| `DB_HOST` / `DB_PORT` / `DB_NAME` / `DB_USER` / `DB_PASSWORD` | MySQL 连接 |
| `MYSQL_ROOT_PASSWORD` | 仅 db 容器初始化使用，不下发 api |
| `UPLOAD_DIR` | 上传文件目录 |
| `MAX_UPLOAD_BYTES` | 单文件上限（默认 2097152） |
| `SESSION_TTL_SECONDS` | 会话有效期（默认 28800） |
| `SEED_TEACHER_PASSWORD` / `SEED_STUDENT_PASSWORD` | 种子账号密码 |
| `LOGIN_MAX_FAILURES` / `LOGIN_WINDOW_SECONDS` / `LOGIN_LOCK_SECONDS` | 限流参数 |
| `TRUSTED_PROXY_CIDRS` | 可信代理 CIDR |
| `WEB_PORT` | Nginx 发布端口（默认 8080） |
| `DIALOGUE_BASE_URL` / `DIALOGUE_MODEL` / `DIALOGUE_API_KEY` | 可选的真实对话模型兼容接口、模型名与密钥；三项必须同时配置且只注入 API。留空时第四课问答使用本地依据摘取生成器，第五课流式会话返回模型不可用 |
| `DIALOGUE_TIMEOUT_SECONDS` / `DIALOGUE_MAX_OUTPUT_TOKENS` | 单轮模型超时与输出上限，默认 60/2048 |
| `HISTORY_MAX_MESSAGES` / `HISTORY_MAX_CHARACTERS` | 保留为旧版配置兼容；新版课件固定末尾6条、每条500字 |
| `EMBEDDING_*` | 可选嵌入服务；三个字段和 `EMBEDDING_DIMENSION` 必须同时配置才启用向量/混合检索 |
| `QDRANT_BASE_URL` / `QDRANT_API_KEY` / `QDRANT_COLLECTION` | 仅在嵌入检索启用时使用；Qdrant 仅供 API 容器访问，不发布宿主机端口 |

## 健康检查与就绪

- `GET /health`：存活检查，不验证依赖，始终 200。
- `GET /ready`：就绪检查，验证 MySQL、上传目录，以及启用向量检索时 Qdrant collection 的维度和余弦距离；不就绪返回 503。Qdrant 临时中断不会影响 keyword 接口，但会使 vector/hybrid 与 `/ready` 降级。

## API 接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/api/login` | JSON `{username, password}` → `access_token`/401/400 |
| `GET` | `/api/me` | Bearer；当前用户信息 → 200/401 |
| `POST` | `/api/logout` | Bearer；撤销当前令牌 → 200/401 |
| `GET` | `/api/materials?q=` | 本班材料列表/搜索 → 200/401/403 |
| `POST` | `/api/materials` | 教师 multipart 上传 → 201/403/400/413 |
| `GET` | `/api/materials/{id}` | 本班材料详情 → 200/404 |
| `GET` | `/api/materials/{id}/file` | 本班文件下载 → 200/404 |
| `POST` | `/api/materials/{id}/reindex` | 本班教师按 auto/custom/hierarchy 重建索引 → 200/400/403/404/503 |
| `POST` | `/api/retrieval/search` | 本班 keyword/vector/hybrid 切片检索，默认 hybrid → 200/400/401/403/503 |
| `POST` | `/api/ask` | 本班 hybrid top 4 有依据问答 → 200/400/401/403/503 |
| `GET` / `PUT` | `/api/assistant/prompt` | 教师读取或保存本班助手提示词 → 200/400/401/403 |
| `GET` / `PUT` | `/api/assistant/skills/guided-problem-solving` | 教师读取或切换解题引导技能 → 200/400/401/403 |
| `GET` / `POST` | `/api/agents` | 列出本班智能体；教师创建智能体与绑定 |
| `GET` / `PUT` | `/api/agents/{id}` | 读取；教师更新角色、启停与两份绑定 |
| `GET` / `POST` | `/api/skills` | 列出本班技能；教师提交原文或上传文件 |
| `GET` / `PUT` / `DELETE` | `/api/skills/{id}` | 读取原文；教师修改或删除技能及绑定 |
| `POST` | `/api/conversations` | 教师/学生选择本班已启用agent_id创建私有会话 → 201/400/401/403/404 |
| `GET` | `/api/conversations` | 教师/学生列出本人本班会话 → 200/401/403 |
| `GET` | `/api/conversations/{id}` | 教师/学生读取自己的消息与来源快照 → 200/401/403/404 |
| `POST` | `/api/conversations/{id}/messages` | 教师/学生通过 Bearer 认证发送2–500字问题，返回 SSE → 200/400/401/403/404/409 |

跨班/不存在资源统一返回 `404 {"error":"not_found"}`。数据库故障返回 503。错误响应不含密钥、正文、堆栈或磁盘路径。

登录使用服务端保存的高熵不透明会话令牌，不是 JWT。浏览器把令牌保存在当前标签页的 `sessionStorage`，并在所有受保护请求中发送 `Authorization: Bearer <token>`；退出或收到认证 401 时清除令牌。命令行调用示例：

```bash
TOKEN=$(curl -fsS http://localhost:8080/api/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"a-student","password":"你的学生密码"}' \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])')

curl -fsS http://localhost:8080/api/me \
  -H "Authorization: Bearer $TOKEN"
```

## 第四课：可追溯知识库检索

材料入库后，MySQL 是正文和授权信息的唯一权威数据源：`knowledge_entries.body_text` 保存完整原文，`knowledge_chunks` 保存切片正文、序号、字符区间、策略和状态。Qdrant 的 point ID 与 `knowledge_chunks.id` 相同，payload 只有 `class_id`、`material_id`、`knowledge_entry_id`、`chunk_id`、`chunk_index`，不复制正文。

教师上传或重建时可选择：

- `auto`：固定最多 800 字、重叠 80 字，优先在空行、换行和句号断开。
- `custom`：长度 100–2000、重叠 0%–50%，可选择分隔符并对索引输入移除 URL/邮箱或合并空白；不会改写原文。
- `hierarchy`：按 Markdown `#`/`##`/`###` 标题分章，超长章节回退到 auto 窗口。

`POST /api/retrieval/search` 从 Bearer Token 读取班级，忽略客户端伪造的班级或角色。keyword 使用 MySQL ngram 全文索引；vector 先嵌入问题，在 Qdrant 按本班过滤并应用 0.35 阈值，再带相同班级条件回 MySQL；启用向量服务时 hybrid 并行执行两路并以 RRF `k=60` 融合，未配置或依赖故障时 vector/hybrid 返回 503，keyword 路径保持独立。响应中的摘录始终来自 MySQL，来源入口继续使用材料详情的同形 404 保护。

`POST /api/ask` 不实现额外检索路径。它固定执行本班 hybrid top 4；零命中直接返回 `资料中未找到相关内容`、空 hits/citations，且不调用回答生成模块。有命中时生成模块只收到当前问题，以及编号后的材料标题、切片序号和切片正文；回答中的 `[1]`、`[2]` 必须对应返回的引用，回答正文还必须与被引切片存在可核验的文本依据。未配置真实模型时使用本地依据摘取生成器，从命中正文中选择与问题最相关的陈述并标注出处；无关输出会被替换为 `无法根据检索到的资料可靠回答`，不会展示伪引用。

检索示例：

```bash
curl -fsS http://localhost:8080/api/retrieval/search \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"query":"第一讲是什么？","mode":"hybrid","limit":4}'

curl -fsS http://localhost:8080/api/ask \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"question":"第一讲是什么？"}'
```

### 已有材料迁移与回滚

数据库迁移是幂等的：先为每份材料保留一条 `body_text` 权威记录，再由 API 启动时以 auto 为没有切片的 ready 材料补索引。重复启动不会复制正文、切片或向量。验收时应核对材料 ID、班级、上传者、原文件校验和、`body_text` 和切片顺序，并抽查 Qdrant point ID 与 MySQL chunk ID 一致。

应用回滚只回退二进制或镜像，不删除 MySQL/Qdrant/upload volumes。旧版本可以忽略新增切片表；原材料和 `body_text` 保持可用。若要重新生成派生索引，使用教师重建接口或启动对账，禁止使用 `docker compose down -v`。

## 第5课：智能体、技能与流式会话

教师登录后进入“智能体与技能”：创建智能体，为它填写角色提示词（可空，最多2000字）、启用状态、技能和本班材料绑定。名称班内唯一，最多64字；空角色使用默认学习助手，不拒绝回答。旧版班级配置保留为兼容接口，新回答只读取会话固定的智能体。

技能文件支持以下课程中文字段格式，也支持标准自包含 SKILL.md（见技能交换说明）。课程格式的名称、适用条件、启用必填，首个空行后是正文；两种格式正文最多12000字符，文件最多16000字符且UTF-8不超过60000字节。示例：

```text
名称：数学解题引导
适用条件：方程,去括号,分数,行程
资料：数学
工具：knowledge_search
启用：是

先检查学生已有的步骤，指出下一步检查什么。不要给最终得数或完整过程，请学生自己完成关键计算，并引用提供的资料。
```

新会话必须选择本班已启用智能体，之后不更换，换助手需新建会话。教师同样可以使用自己的私有会话。停用智能体只阻止新会话选择，已有会话仍能追问；每轮重新读取最新角色、技能和材料绑定，生成中途更新不改变该轮快照。

路由只在绑定且启用的技能中做关键词计分（逗号、顿号、分号、竖线切词，统一小写），最高分胜出，同分较小ID，零分不选技能。检索在SQL、Qdrant、回SQL三个阶段均只限本班绑定材料，取前4条再按技能资料字段过滤标题。证据最多800字，进入用户消息；角色/技能/边界进入系统消息。无资料也继续生成，引用为空，数字引用编号在输出前跨chunk剥除。模型历史取末尾6条完成消息，每条最多500字，含当前问题且不重复。

消息 `POST /api/conversations/{id}/messages` 接受2–500字 `text`，使用 Bearer 和 fetch 读取 SSE：

| 事件 | 内容与时机 |
| --- | --- |
| `meta` | 最先发送；status:ok、技能或null、候选引用、会话/消息ID |
| `delta` | 新增文本片段，多次到达 |
| `done` | 数据库提交后发送助手消息ID、completed与引用 |
| `error` | 流开始后失败终止，不再done |

检索失败在meta前返回JSON503；同会话并发409，停止/断开会取消生成、保存cancelled并清锁。只有done才算完成，刷新可恢复持久化历史。


## 从旧版迁移

旧版使用 Flask + SQLite + Argon2id。迁移步骤：

1. 停止旧服务写入。
2. 备份 SQLite 数据库和上传文件（`internal/migrate.Backup`）。
3. 启动新栈（`docker compose up --build -d`），新库自动迁移和补种子。
4. 运行迁移工具将旧数据导入新 MySQL：
   ```bash
   docker compose run --rm api /server migrate \
     --sqlite /path/to/campusclaw.sqlite3 \
     --uploads /path/to/instance/uploads
   ```
5. 验证：账号数、材料数、文件校验和一致，旧密码可登录。
6. 旧会话不迁移，切换后所有用户重新登录。

**回滚边界**：新栈开始写入前可恢复旧版本与原卷。若新栈已产生业务写入，不能直接切回旧 SQLite，必须先备份新数据并制定反向迁移。禁止将 `docker compose down -v` 当迁移步骤。

## 架构

```
浏览器 → Nginx (web) → Go API (api) → MySQL (db)
                                      ├→ Qdrant（可选内部向量索引）
                                      └→ 对话/嵌入模型接口（仅 API）
```

- 只有 web 发布宿主端口；模型密钥、Qdrant 与 MySQL 不暴露给浏览器。
- API 使用普通 DB 账号，无管理员凭据。
- 上传卷仅 API 挂载。
- `/uploads` 不走静态或 SPA fallback。
- 单 API 实例，限流状态在进程内。
- 会话只属于创建它的学生；同班其他学生、其他班级和教师读取学生会话均与不存在对象一样返回 404。

## 开发

后端测试需要一个专用的空 MySQL 数据库。测试会清空 `TEST_MYSQL_DSN` 指向的库，绝不能指向正在使用的 `campusclaw` 数据库：
```bash
cd backend
docker run --rm --network host \
  -e TEST_MYSQL_DSN="campusclaw_test:pw@tcp(127.0.0.1:3306)/campusclaw_test?parseTime=true" \
  -v "$PWD:/src" -w /src golang:1.26.1-alpine \
  go test ./... -p 1
```

前端开发：
```bash
cd frontend
npm install
npm run dev   # Vite dev server with proxy to :8080
```

## 范围与不做项

- 单 API 实例，不引入 Redis 或消息队列；Qdrant 仅用于可选的向量检索。
- 无公网基础设施，无独立管理后台。
- 无 PDF/Office 通用文件预览（仅 txt/md 正文展示）。
- 不包含作业提交、批改、错题讲解、异步队列、关怀式对话、会话共享或学生自定义提示词/技能。

## Swagger 与第3–6课接口验收

启动应用后打开 `/swagger/`，OpenAPI JSON 位于 `/openapi.json`。页面使用随 Go 程序发布的 Swagger UI 5.32.0，不依赖运行时 CDN。OpenAPI由处理器注解生成。后端执行 `go generate ./internal/apidocs` 可重建文档，Docker构建自动执行；schemas模板位于 `backend/internal/apidocs/contracts.json`。先试调 `POST /api/login`，复制 `access_token` 到 Authorize（只填令牌原文），即可试调受保护接口。教师可管理智能体、技能及绑定；教师和学生均可创建自己的会话；学生访问教师接口返回 403。SSE 的事件结构和启动前/后的错误语义在消息接口中说明，逐段显示由学生页面和接口测试验证。

推荐从仓库根目录用独立测试栈一键验收（需要 Docker Compose，宿主机不需要安装 Python 第三方包）：

```bash
python3 apitest/run_all.py --compose
```

它创建独立项目 `campusclaw-lesson5-apitest`，使用独立 MySQL 数据库/卷、Qdrant、模拟嵌入与对话服务、实际 Go API 和 Nginx。31 个脚本按 URL 与 HTTP 方法组织，覆盖全部业务接口及文档/健康接口；同一接口包含成功、无认证、角色/所有权/跨班拒绝和非法输入等适用场景。JSON 返回按发布的 OpenAPI Schema 校验，登录会话、材料正文/切片、提示词版本、技能状态、会话消息/引用分别与 MySQL 核对。消息验收还检查多轮历史、技能开关、无命中、非法引用、模型失败、明确最终答案、并发 409、主动断开和代理增量到达。

报告生成于 `apitest/reports/report.html` 和 `results.json`，失败退出码非零；该目录被 Git 和 Docker build context 忽略。测试秘密只写入本机 `apitest/reports/.env`（创建权限 0600），不写入代码、报告或镜像。默认结束后清理独立测试栈和其卷，当前应用栈不受影响。测试上传文件仅存在测试卷；使用 `--keep-stack` 时暂时保留，用完后清理整个独立栈。

若需要浏览器检查，运行：

```bash
python3 apitest/run_all.py --compose --keep-stack
```

Swagger 地址为 `http://localhost:18080/swagger/`。独立栈的登录密码在它自己的本机 `.env`，账号仍为 `a-teacher`、`a-student` 等。正常新建会话需选择本班已启用智能体，角色提示词可以为空；测试用例的临时班级会在结束时清理。

在保留的独立栈内执行 Go/MySQL 全量测试：

```bash
docker compose --project-name campusclaw-lesson5-apitest \
  --env-file apitest/reports/.env -f compose.apitest.yaml \
  run --rm backend-tests
```

现有 Go 数据库测试会清空测试表，因此使用另一数据库 `campusclaw_unit_apitest`，并以 `-p 1` 顺序运行包，不能指向业务数据库。该库由独立 MySQL 的初始化脚本创建。

清理保留的独立栈：

```bash
docker compose --project-name campusclaw-lesson5-apitest \
  --env-file apitest/reports/.env -f compose.apitest.yaml \
  down --volumes --remove-orphans
```

模拟模型验收只证明编排、接口和数据一致性，不代表真实模型的引导质量。真实模型还需用相同数值题验证：技能开启只给步骤、不给最终数值；关闭后回答方式明显不同；提示词更新从下一轮生效；无资料仍按智能体角色作答且不编造出处。普通自然语言无法由规则完全证明不含答案，当前规则只额外拦截明确答案声明。

真实对话模型接口验收可复用独立栈。在本机 `apitest/reports/.env` 配置真实的 `DIALOGUE_BASE_URL`、`DIALOGUE_MODEL`、`DIALOGUE_API_KEY`，然后执行：

```bash
python3 apitest/run_all.py --compose --real-model
```

该模式使用真实对话模型、模拟嵌入和独立 Qdrant/MySQL，会调用模型服务；仍然不使用业务数据。它额外核对示例数值题开启技能时不泄露最终数值、提供可继续作答的提示，以及开关前后回答不相同。模型行为验收仍应人工检查报告和浏览器中的代表性回答。


### 新版增量迁移

新增 `tutor_agents`、`skills`、`tutor_agent_skills`、`tutor_agent_materials`；旧会话增量增加 `agent_id`，旧消息保留内容、状态和引用，并增加技能留痕。首次迁移把旧班级提示词复制至默认智能体、关联旧会话，并绑定原班材料；后续启动不会覆盖教师编辑或恢复已经删除的绑定/技能。新智能体只检索明确绑定的资料，新上传资料需要教师手动绑定。

旧 `/api/assistant/prompt` 和固定开关仍为deprecated兼容接口，只同步兼容默认智能体；新页面通过 `/api/agents` 与 `/api/skills` 管理配置。课堂验收和新功能应使用新接口。

新增专项真实模型检查：在已运行真实模型独立栈时执行：

```bash
docker compose --project-name campusclaw-lesson5-apitest \
  --env-file apitest/reports/.env -f compose.apitest.yaml -f compose.apitest.real.yaml \
  run --rm --entrypoint python runner apitest/accept_real.py
```

它记录数学数值题和语文默写的真实回答，检查语文默写时引导不直接给诗的下一句、停用后答案变化和材料注入；结果在本机 `apitest/reports/real-model.json`，不入Git。整个自动套件可独立验证隔离规则；课程要求的同伴本人账号交叉验证仍需由同伴执行并记录，不能以自动脚本冒充。


## 可分享和导入的技能

支持课程格式及标准自包含`SKILL.md`，教师可解析预览、配置本应用关键词、保存绑定多个智能体，并下载标准文件用于其他agent。提供[英语作文批改技能](skills/english-writing-review/SKILL.md)，使用步骤、依赖边界及新接口见[技能交换说明](docs/portable-skills.md)。外部工具声明不自动授权，脚本与附件需适配。

## 第6课：MCP与工具接入

同仓 `mcp-server/` 提供两个只读教学工具，`formula_hint`给公式与步骤提示，`unit_convert`给单位换算方向/倍率，由学生完成数值计算。`POST /mcp` 使用JSON-RPC 2.0，支持initialize、tools/list、tools/call以及initialized通知；`GET /health`不需要令牌。Compose服务名为 `mcp-demo`，仅在内部网络监听8090，不开放宿主端口。现有 `docker compose` 或课堂入口 `docker compose -f docker-compose.yml` 均启动同一套应用。

在本机 `.env` 增加随机 `MCP_AUTH_TOKEN`（至少16字符），只在启动时分别注入API和工具容器，禁止写进代码或Dockerfile。可用 `python3 -c 'import secrets; print(secrets.token_urlsafe(32))'` 在自己的终端生成，再填入 `.env`；不要把生成值贴进仓库或截图。主机和自建服务使用同一值，新增其他服务器时分别使用专属令牌。

教师登录后打开 `/mcp`（导航“MCP 工具”）：

1. 登记 `campus-tools`，Base URL填 `http://mcp-demo:8090`，勾选使用本机环境令牌，保存后点击“连接并发现”。能看到两个工具的完整说明、输入结构与确认状态。
2. 在“智能体与技能 → 技能文件”选择技能，直接勾选“公式提示”“单位换算提示”，无需手写工具引用。选择“自动判断”由模型按需调用，或“必须调用”要求首轮至少请求一个所选MCP工具（不保证全部调用或执行成功）。例如适用条件为“单位,路程,匀速”，保存并绑定智能体。先连接再保存，否则返回可读400。接口不支持工具决策或忽略必须调用时会显示拒绝原因。
3. 学生选择该智能体新建会话，提问“速度每小时3公里，运动2分钟；先提示怎样统一单位，再提示路程公式，不直接算结果”。由模型决定工具与参数，宿主按发现的Schema校验后带令牌执行，结果以tool消息回传，不进入system提示。
4. 对话首字前显示工具引用、轮次及成功/被拒/出错。教师在MCP管理页查看本班倒序调用记录，含用户、会话、消息、时间、轮次和摘要。刷新聊天保留调用快照。
5. 断开会清空工具清单；已有技能下一轮会明确显示denied并留下拒绝记录，仍继续回答。删除/改名后的失效引用也在会话显示denied，但不伪造服务器归属。连接失败不阻断旧检索/聊天路径。

每轮仅向模型提供本次匹配技能的工具声明，名称、说明、输入结构均来自发现。默认最多3轮、每次提问6次、每会话累计12次调用，分别由 `MCP_MAX_ROUNDS`、`MCP_MAX_TURN_CALLS`、`MCP_MAX_CONVERSATION_CALLS`配置；达到上限时撤下工具，基于已有信息继续流式回答。参数无效、远端报错/超时均作为失败数据回传。工具返回命中已知指令式注入时整条丢弃并记录error；句式检查不代表完整的自然语言安全保证。

令牌通过SESSION_SECRET派生的AES-GCM密钥加密落库，列表仅返回 `token_configured`。前端密码输入提交后清空，令牌不回显、不传给模型、不记入日志。SESSION_SECRET轮换后须重新配置MCP令牌。URL限制在 `MCP_ALLOWED_BASE_URLS`逗号分隔的精确允许列表内（默认同网服务）；重定向不携带令牌继续访问。发现的工具限64个，每技能最多16条引用，每项输入Schema最大16KB且禁止外部Schema加载。

初次连接视为确认允许地址上声明为只读且非外发的工具。再次发现说明、结构或安全声明改变时，保存完整前后变更，暂停该工具批准；教师查看后逐工具重新确认。写入/外发类工具不会自动执行，本课只提供教学提示工具。

| 接口 | 用途（均须教师Bearer） |
| --- | --- |
| GET / POST `/api/mcp/servers` | 列表 / 登记；不返回令牌 |
| PUT / DELETE `/api/mcp/servers/{id}` | 修改并断开 / 删除配置保留历史 |
| POST `/api/mcp/servers/{id}/connect` | 握手、发现并整体替换缓存 |
| POST `/api/mcp/servers/{id}/disconnect` | 断开并清空缓存 |
| GET `/api/mcp/servers/{id}/tools` | 完整工具声明与批准状态 |
| POST `/api/mcp/servers/{id}/tools/approve` | 逐工具重新确认，JSON `names`数组 |
| GET `/api/mcp/tool-calls` | 本班最近100次调用审计 |
| GET `/api/mcp/tool-changes` | 本班最近100条完整声明变更 |

SSE兼容扩展：初始meta仍包含技能、引用和消息ID；有调用时在第一个delta前继续发送同ID的meta，`tool_calls`为累计快照（不是追加片段）。每条含 `ref/status/text/round`，status仅 `ok/denied/error`；确无调用时省略字段。前端拒绝正文之后才到的meta、身份变化和工具快照回退。

验收命令：`python3 apitest/run_all.py --compose --keep-stack`；真实模型可用 `--real-model`（使用本机忽略目录 `apitest/reports/.env` 的真实对话配置，嵌入仍模拟）。两个模式均连接真实同仓MCP服务；43个URL组织脚本核对响应、数据库及原有业务回归，HTML在忽略目录。独立MCP协议测试：在Python环境进入 `mcp-server/` 执行 `python3 -m unittest -v`。课堂的同伴本人账号交叉验证需实际同伴参与，自动化跨班测试不替代该环节；详见本次OpenSpec验证记录。

真实模型补充样例（独立测试栈，先配置本地测试.env）：

```bash
docker compose --project-name campusclaw-lesson5-apitest --env-file apitest/reports/.env -f compose.apitest.yaml -f compose.apitest.real.yaml build runner
docker compose --project-name campusclaw-lesson5-apitest --env-file apitest/reports/.env -f compose.apitest.yaml -f compose.apitest.real.yaml run --rm --entrypoint python runner apitest/accept_mcp_real.py
```

小上限验收：给同一个独立项目额外加 `-f compose.apitest.limits.yaml`，仅 `up -d --no-deps api`，然后运行上述脚本并加 `--limits`。验收后去掉limits覆盖文件重新启动api，恢复默认配额。该覆盖只用于独立测试环境，不用于主应用。真实样例结果分别保存到忽略目录的 `week6-real-model.json`（两轮、错误、注入、断开4项）和 `week6-real-limits.json`（1次成功后会话配额拒绝2项）。不要在后端数据库测试执行期间用 `--force-recreate db` 重建同一独立项目。

## 阶段性验收

[验收报告正文](docs/stage-acceptance.md)逐项解释提示词、检索、Skill与MCP的实际LLM参数，附无凭据测试请求/返回快照与复现方式。本次报告不生成PDF。
