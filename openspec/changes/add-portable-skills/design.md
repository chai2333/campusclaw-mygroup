# Design

## Context

当前skills表保存解析字段和source_text，同一skill_id可以绑定多个本班agent。运行时仅一个技能正文进入模型上下文。现有课程格式继续可读写。

## Goals / Non-Goals

支持自包含SKILL.md的保存与双向导出，不把跨产品文件格式兼容误称为相同执行环境。ZIP、附件下载、Shell/脚本执行和外部工具自动安装不在本次范围；UI和文档明确提示。

## Decisions

- 用固定版本yaml.v3解析frontmatter，拒绝别名、重复键、多文档及过深结构；所有输入有字符/字节上限。
- 标准name保持1–64个小写字母/数字/连字符且无连续连字符；应用显示名仍最多32字，长标准名可调整显示名，不更改原始name。
- description用于标准技能发现；本应用仍依赖教师明确配置的关键词，不把整句description当成关键词。普通外部技能默认停用，缺关键词可预览、不可保存。
- metadata的campusclaw-display-name/triggers/subject/tools/enabled存放本地设置。原有license、allowed-tools和其他元数据保留；allowed-tools不映射到MCP授权。
- POST /api/skills/preview返回解析字段、规范化原文、格式、警告及ready状态，不落库。GET /api/skills/{id}/export返回JSON文件描述，浏览器生成SKILL.md下载，便于Bearer授权与错误处理。
- 保存仍走既有事务MCP引用校验。导入文件始终开启新建草稿，防止覆盖当前选中的技能。使用同一导入设置流程修改标准技能开关。

## Risks / Trade-offs

- 辅助文件未随单文件上传 → 显示依赖提示；作者需合并必要参考文本或将功能适配为MCP。
- 外部提示词可能含越权指令 → 继承平台规则、候选工具集合、参数和班级权限检查；不声称可识别所有自然语言注入。
- 更长正文增加模型上下文 → 明确12000字符上限，仅匹配技能进入本轮，不载入所有技能。

## Migration Plan

无需SQL迁移。先独立栈验证，再更新主应用API和web，保留原有数据、绑定和环境文件。用户示例通过本机API创建，不提交业务数据库或凭据。
