# Proposal

## Why
第6课要求把真实MCP工具服务接进第5课的多智能体流式会话。现有项目只有固定检索和文本生成，缺少发现、模型工具决策、授权执行和审计。

## What Changes
- 同仓新增mcp-server/，Compose启动真实HTTP JSON-RPC服务，Bearer验证，formula_hint/unit_convert两个只给提示的工具。
- 教师登记/修改/删除服务器，握手与发现，缓存替换和断开，完整说明/结构展示；变更重新确认；令牌仅服务端加密保存，不返回。
- 技能支持tool@server，保存校验本班、连接、发现和授权。模型获得发现的候选声明，宿主校验参数、逐次复核、回传工具消息并控制轮次/累计次数，失败局部降级。
- 增量meta显示调用结果，持久化与历史恢复；教师查看本班调用及发现变更。OpenAPI注解、URL组织接口测试、真实服务/真实模型验收和备份升级。

## Capabilities
### New Capabilities
- `mcp-tools`: 服务器协议、班级登记发现、技能引用、模型调用循环、信任边界和审计。
### Modified Capabilities
无已归档主规格；保留第3–5课在途规格及行为，新增规格明确其增量。

## Impact
Go API/MySQL增量表、dialogue模型客户端、conversation编排/SSE、React管理/聊天、OpenAPI、Compose和apitest。已有数据与接口保留，真实密钥/报告/备份不提交。课堂同伴本人账号验证需真实同伴，不能用自动化冒充。
