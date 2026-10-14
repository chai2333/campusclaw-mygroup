# 第5课新版补齐验收记录

日期：2026-10-14（Asia/Shanghai）。以当日课件的多智能体、技能文件、绑定检索与流式协议为准。现有 OpenSpec 方案已更新，实现已部署到本机真实应用 `http://localhost:8080`。此前未提交的工作保留。

## 实现范围

- 教师创建、编辑、启停多个智能体，各自维护角色提示词和本班技能/材料绑定；学生只看到已启用智能体。跨班、未知或学生访问停用智能体均返回同一404。
- UTF-8技能文件上传、编辑、删除、启停，保存原文；验证字段、字数和工具名，按适用条件关键词选择已绑定且启用的至多一个技能。
- 教师和学生各自拥有私有会话；创建时选择智能体且之后固定。停用不阻断已有会话；配置修改在下一轮生效。
- 检索在关键词SQL、Qdrant及混合路径源头限定材料，绑定为空不调用检索；证据最多4条，先过滤再编号。系统指令与用户材料证据分开，无命中也回答且不编造编号引用。
- 提问2–500字，首次标题24字，最近6条有效历史每条最多500字。meta先给技能/引用，delta增量显示，数据库提交后done；保留并发锁、取消和失败持久化。
- Bearer认证与文档一致。OpenAPI由处理器注解生成，20条路径、27个业务操作，与路由注册匹配；本地Swagger可打开。

## 验收结果与证据

| 验收 | 结果 | 本机证据（Git忽略） |
| --- | --- | --- |
| 模拟模型完整接口脚本及响应/数据库一致性 | 31/31通过 | `apitest/reports/report-mock.html`、`results-mock.json` |
| 真实course-chat完整接口脚本 | 31/31通过 | `apitest/reports/report-real.html`、`results-real.json` |
| 独立MySQL全后端测试 | 全部包通过，数据库测试实际执行 | `apitest/reports/week5-new-backend.log` |
| 前端协议测试 | 15/15通过，包括碎片UTF-8、meta/终止校验、错误与取消 | `npm --prefix frontend test` |
| 前端类型检查和构建 | typecheck、typecheck:test、build通过；最后UTF-8修正再次检查/构建通过 | 本机命令输出 |
| OpenAPI | OAS规范验证和实际响应Schema验证通过；27个操作与注册路由匹配 | 接口报告与注解生成器 |
| 真实数学/语文、开启/关闭技能、材料注入 | 4/4通过 | `apitest/reports/real-model.json` |
| 浏览器教师/学生 | 创建数学和语文助手、绑定差异、学生选择、真实增量回答、刷新恢复固定助手及技能 | `multi-agent-teacher.jpg`、`multi-agent-student.jpg` |
| 窄屏 | 320像素宽聊天、教师配置、技能编辑器无页面横向溢出，测试后恢复尺寸 | 浏览器DOM测量宽度=scrollWidth=320 |

接口脚本按URL与HTTP方法组织在 `apitest/cases/`；覆盖新增智能体/技能及旧登录、资料、检索、单次问答等接口，含跨班隐匿、绑定原子更新、文件格式错误、路由打分、空绑定/无命中、历史、停用已有会话、并发409、失败/取消与恢复。HTML报告不提交Git。真实模型独立环境使用模拟嵌入与独立MySQL/Qdrant，不能将其视为完整真实向量网关的端到端验收；本机8080配置实际course-embedding（2048维）和course-chat。

真实模型代表结果：数学开启技能只给公式及单位检查、由学生算最终值；关闭技能给出12米。语文开启技能提示回忆意象，关闭技能给出“处处闻啼鸟”。恶意材料中的泄露指令和canary未进入回答。引导质量及自然语言安全边界由代表样例验证，规则不能证明所有任意输出均无最终答案。

## 升级与数据保留

- 升级前已备份：`backups/before-multi-agents-20261014.sql`，权限0600，Git忽略。
- 原提示词和历史会话一次性迁移到默认助手；重复迁移测试验证不会恢复删除的技能/绑定或覆盖教师修改。
- 升级前后原业务计数一致：账号6、资料2、会话10、消息30。证据：`business-counts-before.txt`与`business-counts-after.txt`。
- 主应用8080健康；`/ready`、`/swagger/`、`/openapi.json`返回200。最后前端校验修正已重新构建并更新本机服务。
- `.env`、测试`.env`、报告及备份均被Git忽略；报告、备份不进入Docker上下文。没有提交密钥。

## 课堂尚需人工完成

课件要求的“同伴本人账号交叉验证”需要真实同伴参与，本次跨班账号自动化和教师/学生浏览器验证不能替代这一课堂环节。实现及开发验收完成，此项课堂人工验收尚未进行。

## 历史记录

下面保留2026-10-13旧版单助手验收，仅作历史证据；其中旧的22项、start协议及未接入真实模型的状态已由上文更新。

<details>
<summary>2026-10-13旧版验收记录</summary>

# 第5课补齐交付验收记录

日期：2026-10-13（Asia/Shanghai）。本记录对应当前工作区代码，保留此前尚未提交的改动。

## 已验证

- 独立 Compose 项目：`campusclaw-lesson5-apitest`。使用独立 MySQL、Qdrant、实际 Go API/Nginx，以及兼容 OpenAI 的模拟嵌入/对话服务；不替换现有 `campusclaw-mygroup` 服务和卷。
- `python3 apitest/run_all.py --compose --keep-stack`：22/22 接口脚本通过。覆盖全部业务接口，另有健康、OpenAPI 和 Swagger 脚本；脚本内包含多个成功/失败场景。
- OpenAPI 3.0.3 描述通过 `openapi-spec-validator`；接口实际 JSON 返回使用 OpenAPI Schema 校验。数据库断言涵盖登录 HMAC 会话、材料正文与切片、检索班级/元数据、提示词版本/更新人、技能状态、会话所有者、问题/回答顺序、引用快照和活动锁。
- 真实 Nginx HTTP 流验收：多个 delta 在终止前到达；成功时 done 在提交后发送；模拟模型故障、明确答案、非法引用、缺少无命中说明产生 error 并保存 failed；并发发送返回 409；断开保存 cancelled，随后可继续提问。
- `go test -count=1 -p 1 ./...`：全部包通过，已配置 `TEST_MYSQL_DSN` 指向独立 `campusclaw_unit_apitest`，数据库测试实际执行。日志在本机忽略目录 `apitest/reports/backend-tests.log`。
- 前端 `npm run typecheck`、`npm run typecheck:test`、`npm test`（12/12）、`npm run build` 通过；初次宿主机 SSE 测试受沙箱回环监听限制，授权环境重跑通过。
- `.venv/bin/ruff check apitest` 通过。报告 HTML 转义、秘密/令牌脱敏、启动阶段失败返回非零及生成报告的故障注入检查通过。
- 浏览器：Swagger 页面正常加载并通过 Try it out / Execute 获得 200；教师登录后保存第1版提示词；学生登录直接进入会话、新建会话、观察增量文本、无命中说明与空引用、刷新后恢复完整历史。截图在本机忽略目录。
- `git check-ignore` 确认 `.env`、测试 `.env`、报告不进入 Git；测试 `.env` 创建权限为 0600，报告目录也从 Docker build context 排除。
- `openspec validate add-guided-ai-streaming-chat --strict` 与 `git diff --check` 通过。

## 修复的真实问题

- 技能非法输入路径原先写入两段错误 JSON；现在只返回一次错误，并拒绝尾随 JSON 对象。
- 消息接口拒绝尾随 JSON 对象，避免请求协议模糊。
- hybrid 未配置向量服务时原先静默回退；原第4课规格要求 503，现已对齐且原依赖故障测试通过。
- 模型证据原先缺少位置；现在包含标题、切片序号和字符范围。
- 学生导航以会话为入口，移除独立检索导航并保留第4课接口/教师诊断路由。
- 解题引导额外检查明确答案声明；无命中回答缺少指定说明时不保存为 completed。规则不能证明任意自然语言不存在答案。

## 尚未验证：真实对话模型

当前项目本机 `.env` 未包含完整 `DIALOGUE_BASE_URL`、`DIALOGUE_MODEL`、`DIALOGUE_API_KEY`；本次没有调用真实模型，也没有把模拟输出视为真实模型证据。

真实模型运行入口已提供：在本机 `apitest/reports/.env` 配置上述三项，然后运行 `python3 apitest/run_all.py --compose --real-model`。此模式使用真实对话模型、模拟嵌入和独立 Qdrant/MySQL，额外核对数值题开启技能时无最终数值、有可操作提示，开关前后回答不同。人工仍需检查实际引导质量、材料注入抵抗和相同问题开关差异；完成前不勾选真实模型验收任务。

</details>
