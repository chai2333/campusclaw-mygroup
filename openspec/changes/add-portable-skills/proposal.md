# Proposal

## Why

课程技能采用中文字段，无法直接交换标准SKILL.md。用户需要制作英语作文批改技能，复用到多个智能体，并导入其他作者的技能。

## What Changes

- 保留课程格式，增加自包含Agent Skills格式的解析、教师预览、应用关键词设置和可下载导出。
- 保留标准文件的名称、说明、正文、许可证及其他元数据；本应用设置放在命名空间metadata中。
- 外部allowed-tools不授予本应用权限；依赖脚本和附件的技能显示适配提示，本次不增加代码执行器或ZIP安装器。
- 提供独立的english-writing-review/SKILL.md，并在实际应用创建可绑定的英语作文批改技能。

## Capabilities

### New Capabilities
- `portable-skills`: 自包含技能文件的双向交换、导入预览和跨智能体复用。

### Modified Capabilities

无已归档主规格；继续保留既有班级隔离、关键词路由及MCP授权约束。

## Impact

Go tutor解析/API、React技能管理、OpenAPI、接口及数据库验证、使用文档和示例技能。添加固定版本YAML解析依赖；不改变数据库结构。正文上限12000字符，源文件16000字符且UTF-8不超过60000字节，仍适配现有TEXT列。
