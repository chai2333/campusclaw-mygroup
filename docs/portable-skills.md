# 技能创建、分享与导入

本应用同时支持课程中文字段格式和标准Agent Skills的自包含`SKILL.md`。同一个本班技能可绑定多个智能体，修改后从各智能体的下一轮提问生效。

## 使用自己的英语作文批改技能

文件位于[`skills/english-writing-review/SKILL.md`](../skills/english-writing-review/SKILL.md)。它解释语法、拼写、搭配和篇章问题，区分必要修正与可选风格优化，保留作者原意；没有量表时不编造考试分数。无需脚本、附件或特定工具。

教师打开“智能体与技能 → 技能文件”，读取这个文件，查看预览后保存。随后到“智能体”页勾选该技能并保存；可在多个智能体中勾选同一项。例如英语写作助手与综合学习助手都可复用它。学生新建对应会话，输入“请批改英语作文：Yesterday, she go to school and buy a apple.”即可触发。

## 给其他agent使用

选中已保存的技能，点击“导出已保存版本 SKILL.md”。浏览器下载标准文件，提示对应目录名。把文件放在同名目录中，例如：

```text
english-writing-review/
  SKILL.md
```

将整个目录按目标agent的技能安装说明放到其发现目录，再通过该agent的技能列表或显式调用使用。是否自动发现、安装位置和可用工具由目标agent决定；没有在其他产品实际安装时，不把格式校验当成对方运行验证。

标准文件的`name`用于跨agent标识，`description`说明用途，正文提供工作流程。本应用的显示名、关键词、资料标题筛选、工具引用和启用状态放在`metadata.campusclaw-*`字符串字段中，不要求其他agent识别。

## 使用别人的技能

1. 获取作者提供的UTF-8 `SKILL.md`，保留其中的许可证和来源元数据。
2. 读取文件会开启一个新草稿，不覆盖当前选中的技能。也可粘贴原文后点“解析预览 / 设置适用条件”。
3. 检查原始头部、完整正文和依赖提示，设置本应用显示名与适用关键词。普通外部文件默认停用，缺关键词可以预览但不能保存；确认要使用时勾选启用。
4. 必要时配置本班MCP工具引用，保存后再绑定智能体。外部`allowed-tools`只保留供查看，不自动授予本应用权限。

本次支持自包含的提示词技能单文件交换，不安装ZIP，不上传辅助目录，不执行Shell/Python等脚本。含`references/`、`scripts/`、`assets/`或其他本地文件依赖的技能会显示适配提示；应将必要参考文字合并进正文，或把实际功能封装为经过本班校验的MCP工具。文件兼容不代表其他agent的运行环境、工具能力也已搬入应用。

## 限制与接口

- 文件最多16000个Unicode字符且UTF-8不超过60000字节；正文最多12000字符。超限明确拒绝，不静默截断正文。
- 本应用显示名1–32字，关键词1–120字，资料筛选最多32字，每技能最多16个工具。
- 标准name为1–64个小写字母、数字或连字符，不能首尾或连续使用连字符；description为1–1024字符。长标准name可配置较短显示名，导出保留原name。
- YAML拒绝别名、锚点、重复键、自定义标签与过深结构。保留许可证、工具声明和未知元数据的语义，重新序列化可能调整排版或移除YAML注释。
- `POST /api/skills/preview`：教师Bearer，JSON `source_text`及可选`name/trigger_text/subject_text/tools/enabled`，返回格式、解析技能、警告与`ready`；不写数据库。
- `GET /api/skills/{id}/export`：教师Bearer，返回`filename/directory/source_text/warnings`。跨班与不存在均404，学生403，未登录401。
- 保存仍使用`POST /api/skills`与`PUT /api/skills/{id}`，保存时重新校验本班MCP引用；预览成功不代表该工具已获授权。

格式参考：[Agent Skills规范](https://agentskills.io/specification)、[OpenAI技能说明](https://learn.chatgpt.com/docs/build-skills)。当前应用使用本地关键词路由，未改为由模型读取description做语义路由。
