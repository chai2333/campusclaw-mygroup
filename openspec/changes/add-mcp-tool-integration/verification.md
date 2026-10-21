# 第6课 MCP与工具接入验收记录

日期：2026-10-21，Asia/Shanghai。基于用户提供的当日课件及任务清单，在既有第3–5课代码上增量实现。未归档旧变更，未删除此前未提交的output/tmp文件。

## 需求与证据

| 项目 | 验证结果 | 本机证据（Git忽略） |
| --- | --- | --- |
| 同仓真实MCP服务 | Python HTTP服务、initialize/initialized/tools/list/tools/call、/health、Bearer；两个只给提示的工具 | `mcp-server/`源码；`apitest/reports/week6-protocol.log`，3/3测试 |
| 登记与发现 | 教师CRUD、本班唯一、缓存整体替换、重复连接不翻倍、错误令牌失败置未连接、断开清空、跨班404/学生403 | 后端MCP集成测试及41接口报告 |
| 令牌边界 | 本机各栈不同随机令牌，运行时注入；服务端AES-GCM加密落库，响应只token_configured，模型不接触凭据；前端密码不回显 | 单元/接口密文与响应断言；最终凭据扫描；三份应用镜像配置/构建历史无密钥匹配 |
| 技能引用与最小权限 | 格式、同班、已连接/发现/确认的保存验证在事务内；仅匹配技能候选声明交给模型，按发现Schema校验参数 | `internal/mcp`、`tutor`测试；技能/对话脚本 |
| 模型原生调用循环 | 工具名称/说明/Schema来自发现；原生tool_calls与tool_call_id关联，多轮后以tool角色回传，最终请求撤下tools | HTTP客户端测试、真实模型样例与浏览器 |
| 安全与配额 | 参数错误不执行；写入/外发声明不自动批准；注入结果整条替换；轮次、单次提问次数、会话累计次数上限；失败仍完成回答 | MCP单元/真实DB测试，真实6项样例 |
| 声明变更 | 规范化JSON比较，记录完整前后说明/结构；变更暂停批准，教师逐工具重新确认后可绑定 | `TestRegistryDiscoveryAndSkillValidation`；工具变更接口 |
| 流式与持久化 | 正文之前累计meta.tool_calls，多轮不重复追加，首字后拒绝meta；调用快照刷新可恢复 | 20/20前端流式测试，HTTP/DB断言，浏览器截图 |
| 审计隔离 | ok/denied/error、本班用户/服务器/工具/会话/消息/轮次/时间/摘要；不存在服务器只记录本轮状态，删除保留既有历史快照 | 接口报告与数据库检查 |
| Swagger/OpenAPI | 处理器注解生成28路径、39操作，与注册路由逐项匹配，OAS及响应Schema验证；浏览器Authorize/Execute得到200 | `week6-swagger.txt`（Bearer已脱敏） |
| 模拟接口环境 | 41/41脚本通过；实际API/Nginx/MySQL/Qdrant与同仓MCP，只有模型/嵌入模拟 | `week6-report-mock.html`、`week6-results-mock.json` |
| 真实模型接口环境 | 41/41脚本通过；真实course-chat和同仓MCP，嵌入仍模拟，独立数据清理 | `week6-report-real.html`、`week6-results-real.json` |
| 全后端回归 | 完整Go包与独立MySQL全部通过；最终MCP/原生模型客户端针对性测试通过 | `week6-backend.log`、`week6-final-targeted.log`、`week6-cancel-final.log`、`week6-migration-final.log` |
| 前端检查 | typecheck、typecheck:test、build、20项协议测试通过 | 本机命令结果；构建日志 |
| 浏览器与窄屏 | 教师登记发现/完整Schema/断开恢复，技能保存绑定，学生两轮真实调用与刷新，教师审计；管理和聊天320px无整页横向溢出 | `week6-mcp-teacher.jpg`、`week6-mcp-student.jpg`、`week6-mcp-audit.jpg` |

`apitest/cases/`按URL和HTTP方法组织；新增10个MCP管理脚本，总计41个，另有真实模型专项脚本。HTML报告、样例回答和截图只保存在本机忽略目录。独立项目仍沿用 `campusclaw-lesson5-apitest` 名称，避免误操作主应用。

## 真实模型专项样例

`week6-real-model.json`四项全部通过：

1. 第1轮unit_convert，读完工具结果后第2轮formula_hint，回答先提示单位换算，再提示s=v×t，没有直接算出原题最终值。
2. 远端工具isError，审计error，模型如实说明工具失败并继续给一般提示。
3. 工具返回已知恶意指令与canary，整条丢弃，审计error，最终回答不含canary。
4. 断开后两个技能引用都明确denied，写拒绝审计，仍完成回答。

`week6-real-limits.json`两项通过：在独立API应用limits覆盖，使轮次/单次/会话上限均为1；首次实际工具调用ok，下一次因会话配额denied且不再执行远端调用，仍完成流式回答。验收后已恢复独立API默认3/6/12配置。真实样例是代表行为证据，不能证明任意自然语言输出均无答案或抵抗所有改写注入。

仅独立MCP测试栈启用MCP_TEST_MODE来制造远端错误与恶意返回；主应用没有启用该测试开关。

## 主应用升级与保留

- 已升级 `http://localhost:8080`，新增同网 `mcp-demo`，不发布其8090端口。`docker-compose.yml`兼容入口与compose.yaml包含同一组服务。
- 备份：`backups/before-mcp-20261021.sql`，43860字节，权限0600，Git忽略。
- 升级前后原业务计数一致：账号6、资料2、会话11、消息32、智能体5、技能12。证据：`week6-business-counts-before.txt`、`week6-business-counts-after.txt`。
- `/ready`、`/swagger/`、`/openapi.json`均200；主文档含MCP新路径。浏览器演示配置/会话只创建在独立18080环境，没有改写主应用业务内容。
- Git检查只提交代码、配置示例、规格、测试和验收说明；真实.env、备份、报告、截图和临时产物保留本机。

## 修复与验证范围

补测客户端取消工具请求：已发动作的审计使用有界独立持久化上下文，取消网络请求仍留下error审计，消息调用快照也独立保存；新增取消回归与原会话全部通过。

重复发现曾因MySQL JSON字段重排而触发伪变更，已改为规范化内容比较并通过重复连接和真实声明变更测试。首次全后端运行与独立数据库强制重建重叠导致连接中断，已在数据库稳定后重跑全部包通过；共享测试库的清理型测试包须使用项目配置的-p 1，最后迁移测试也按此串行复核；未将基础设施中断伪装为代码通过。

MCP服务是本课要求的无状态HTTP JSON-RPC工具子集，协商2025-03-26，不实现resources/prompts、通用SSE远端传输或会话型MCP服务器的全部扩展。自动执行限定为明确只读且非外发的教学提示工具；高副作用工具没有自动授权通道。URL允许列表默认只有同网自建服务，新增第三方地址须维护者显式配置。

## 课堂人工验证仍需完成

课件要求同伴本人账号在同一环境交叉验证四项：服务器/工具隔离、令牌不回传、断开即失效、审计范围。自动化跨班身份和浏览器教师/学生路径已验证相关行为，但没有冒充真实同伴；这四项课堂人工参与及其截图/日志仍需本人邀请同伴完成。

## GitHub同步

按AGENTS.md持续约定，检查与验收完成后提交并同步本仓库，不强制推送。发布结果在最终回复给出；验收记录后续附实际提交标识。
