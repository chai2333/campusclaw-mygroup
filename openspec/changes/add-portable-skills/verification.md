# 技能交换验收

日期：2026-10-21，Asia/Shanghai。实现对应用户确认的两个方向：应用内多智能体复用、标准自包含SKILL.md交换；首个自定义技能为英语作文批改。

## 自动化与实际操作

- 全Go包在独立MySQL环境串行通过（`-p 1`），包含新增解析、导出往返、YAML边界和同一技能绑定两个智能体并进入实际编排上下文的测试。最终重新生成OpenAPI并复核apidocs/tutor/conversation通过。日志位于忽略目录`apitest/reports/portable-backend.log`和`portable-final-go.log`。
- 43/43接口脚本通过，真实API、MySQL、Nginx、Qdrant和同仓MCP，模型/嵌入模拟。新增预览和导出脚本核对预览不写库、保存与数据库一致、原文元数据、启停、往返和教师/班级权限。`portable-mock-results.json`、`portable-mock-report.html`仅存本机。
- 前端build/typecheck:test通过，流式协议20/20通过。首次直接运行被沙箱禁止监听临时端口，允许本机监听后同一测试命令通过，没有当作产品失败修复。
- skill-creator的quick_validate校验仓库英语技能及浏览器实际下载的SKILL.md均通过；本机校验器依赖PyYAML安装在Git忽略的.venv，不进入产品依赖。
- 浏览器在18080实际读取英语文件、查看预览、保存并下载文件，下载文件通过独立格式校验。再读取第三方文件时显示新建草稿，原英语技能仍存在；修改显示名/关键词/开关后保存独立新技能成功。

## 真实模型专项

`accept_portable_real.py`使用独立真实course-chat环境，嵌入仍为模拟；三项PASS，结果在`portable-real-model.json`。两个本班智能体绑定同一份英语技能，SSE与数据库skill_id一致，均纠正went/bought/an apple/I agree，给中文原因。另验证跨班绑定404。

第三项从本机忽略目录读取Anthropic公开的[frontend-design](https://github.com/anthropics/skills/tree/main/skills/frontend-design)文件，设置本地关键词后实际保存、绑定和生成设计建议，数据库与SSE均选择导入技能。原始文件SHA256为`d91970639e9f5c37682ac7ab60094d35f1c7c1f38d731bd56396563aee10c1d3`，不复制第三方完整正文到Git。测试是单文件提示词加载与回答验证，不声称在本应用运行了外部编码工具、脚本或浏览器构建流程。

初次英语样例暴露可选衔接改写擅自补充原文逻辑关系，已加强指代不明时保留原句和不虚构目的/因果关系的指令，并复测语法与固定禁用改写断言。完整输出保留供人工判断，测试不保证任意模型输出均无语义偏差。

## 主应用交付

- 已更新8080主应用API与web，ready/Swagger/OpenAPI均200，新契约包含两个接口。
- 部署前只读备份`backups/before-portable-skills-20261021.sql`，53451字节、0600，Git忽略。
- 部署前后账号6、资料2、会话12、消息36保持一致。按用户需求新增一条英语作文批改技能和一个英语写作助手，技能12→13、智能体5→6，未重绑其他智能体。
- 主应用A班英语技能id=13、助手id=6。临时API登录已注销，技能正文/源文SHA256与MySQL一致，导出再预览正文一致。浏览器确认主应用助手已启用并勾选英语技能，截图`portable-main-agent.png`仅存本机。

## 边界与来源

支持自包含SKILL.md，正文最多12000字符，源文件最多16000字符且60000 UTF-8字节。脚本、ZIP、附件目录和其他产品执行环境不安装或运行；依赖提示明确显示，外部allowed-tools不授予本应用权限，MCP引用保存时仍校验本班可用性。文件格式校验不冒充已在其他agent产品中安装运行。

格式依据：[Agent Skills规范](https://agentskills.io/specification)、[OpenAI技能文档](https://learn.chatgpt.com/docs/build-skills)。YAML使用固定依赖gopkg.in/yaml.v3 v3.0.1，拒绝别名、重复键、自定义标签和过深结构。

所有真实环境文件、数据库备份、测试报告、下载文件和截图不加入Git；保留原有未提交output/tmp目录。发布结果以最终提供的GitHub提交链接为准。
