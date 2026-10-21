# Design

## Context
现有Go net/http+MySQL，React和SSE，技能按关键词路由，dialogue.Gateway仅流式文本。第6课需两端真实接通且不损坏已有问答。

## Goals / Non-Goals
Goals: 可追溯的真实只读提示工具，多轮模型调用，严格班级/凭据边界，兼容旧路径。
Non-Goals: 第8课指标追踪；本次不开放有外部副作用的工具自动执行。

## Decisions
- 同仓Python标准库MCP服务，HTTP JSON-RPC单端点，两个只读提示工具。compose.yaml为现有入口，提供docker-compose.yml兼容入口。
- 新mcp_servers/mcp_tools/mcp_tool_changes/mcp_tool_calls表；服务端AES-GCM密文令牌，密钥派生自SESSION_SECRET，响应只布尔。URL精确允许列表避免带凭据任意出站，默认同网mcp-demo:8090。教师可提交密码字段或选择服务端.env令牌，不回显。
- discover原子替换，version乐观检查防断开/改名期间过期发现重新连接。保留完整工具版本变更供教师确认。新工具须只读声明才可批准；changed工具暂停直到显式确认，副作用工具拒绝自动执行。
- 用JSON Schema库验证发现结构及模型参数；禁止外部Schema加载。函数别名由服务器ID+工具名生成，映射在宿主，模型不见URL/令牌。HTTPGateway新增非流式Decide接口，旧Gateway和旧检索问答保持兼容。
- 模型工具决策循环在最终Stream前，最多3轮、单次提问6次、会话累计12次，均可配置；达到任一上限最终请求不带tools。失败也回tool消息。断开引用预检显式denied，不将失效引用静默删除。
- meta可在首个delta前同ID/技能/引用重复推送，tool_calls为累计快照，前端覆盖不重复累加；最后落message工具快照，失败/取消也保留。
- 审计正文/原因有限长，不存令牌或原始模型参数，删除服务器保留历史快照；不存在/越权引用仅本轮应答记录拒绝，不伪造服务器归属。

## Risks / Trade-offs
句式检查不完备→数据位置、逐工具授权与硬配额作为实际防线。工具Schema/描述不可信→限长/只读批准/变更再确认。模型不选工具属正常→确定性循环测试与真实模型样例分开验收。SESSION_SECRET轮换→旧密文不可解，教师需重配令牌。

## Migration Plan
先独立Compose验收；备份原数据库与业务计数，增量表/列幂等迁移，生成本机随机MCP令牌而不打印，重建主应用，核对健康/数据/浏览器路径，验证后提交并同步GitHub。
