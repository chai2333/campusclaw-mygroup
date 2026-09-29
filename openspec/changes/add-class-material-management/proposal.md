# Proposal

## Why

CampusClaw 迭代 1 需要完整覆盖第 3 课的身份授权、班级隔离、材料入库与 Web 体验，并让第三方能够通过 Compose 复现。现有 Flask + SQLite 版本已实现基础能力；本次按用户选择切换到课件技术栈，并将缺失的行为与失败路径写成可验收合同。

## What Changes

- **BREAKING**：以 Go + MySQL 8.0 后端、React 18 + TypeScript + Vite 前端及 Nginx 入口替换 Flask 模板与 SQLite；标准部署为 web/api/db 三服务，默认入口改为 `http://localhost:8080`。
- 保留 JSON 登录、当前身份查询、注销、服务端持久化会话及教师/学生两种角色；增加统一登录失败响应、等价密码比较和用户名＋IP 失败限流。
- **BREAKING**：忽略客户端班级声明，所有数据归属由会话关联用户决定；跨班详情及下载与不存在的材料统一返回同形 `404`。
- **BREAKING**：仅接受非空 UTF-8 `.txt/.md`；同步上传成功返回 `201`，失败时材料表、知识条目表和文件均不得留下该次上传的残留，不再保存失败材料行。
- 补齐本班材料列表、标题/正文搜索、详情与鉴权下载；知识库只存储可查询正文，不引入向量检索或问答。
- 补齐登录页与材料页、依据 `/api/me` 恢复身份、401 回登录、北大红品牌、深浅主题、响应式布局、列表/网格、命令面板、上传进度、toast 及安全 GFM 展示。
- **BREAKING**：`/health` 改为仅表示进程存活；新增 `/ready` 表达数据库和上传目录就绪；数据库故障的业务请求返回 `503`，不能误报登录失效。
- 增加双班可区分材料种子、已有 SQLite 数据与原文件迁移、旧会话失效及回滚方案；不删除既有业务数据或重置既有密码。
- 将干净环境启动、持久化、负向测试、ADR、同伴互验、版本标识与最终归档纳入交付流程。

## Non-goals

- 不实现向量检索、跨班全文检索、RAG、知识库问答或模型调用；本班普通文本筛选属于本次范围。
- 不实现对话助手、作业布置/提交/批改、成绩与错题本。
- 不实现平台超级管理员、用户自助注册/改密、账号管理、多校多租户、验证码或邮件短信认证。
- 不实现 JWT、Bearer token、OAuth 或校园 SSO；客户端不得持久化会话 ID 或权限声明。
- 不实现 PDF、Word、图片解析及文档编辑器；本次 `.txt` 正文和 `.md` GFM 详情展示属于范围。
- 不实现 Kubernetes、CI 平台、公网发布、域名/HTTPS 证书配置、多副本或生产级高可用；非开发环境仍须正确设置 Secure Cookie。

## Capabilities

### New Capabilities

三个能力均尚未归档为主规格；继续在既有 delta 文件内修订，不新增能力文件。

- `identity-and-role-access`: 登录与限流、角色授权、服务端会话、种子、运行配置、探针、部署、数据迁移及可复核交付。
- `class-data-isolation`: 列表与搜索的班级过滤、对象详情和文件隔离、上传归属、无班级身份拒绝及脱敏审计。
- `class-material-ingestion`: 文件校验、原子入库、正文与来源信息、列表/搜索/详情/下载及完整 Web 行为与体验。

### Modified Capabilities

无。

## Impact

- 后续实施将新增 `backend/`、`frontend/` 和 Nginx 配置，重建 Compose、Docker 构建、测试与 README；旧 Python 应用在迁移验证后退出标准运行路径。
- 新后端使用 MySQL 驱动、bcrypt、兼容既有 Argon2id 的校验器；前端使用 React Markdown、Remark GFM 和原生 CSS。具体版本在实施时验证兼容性并锁定。
- MySQL 保存用户、班级、会话、材料与知识条目；原文件存于仅 API 挂载的数据卷。迁移工具需保留原始 ID、哈希、班级、材料来源及正文顺序。
- 环境变量以 `SESSION_SECRET`、数据库凭据、种子密码、上传限制、会话 TTL、限流参数和 `WEB_PORT` 为入口；真实值不写入规约、仓库或镜像。
- 原实现的通过记录仅证明旧版本。新栈必须重新通过全部行为、浏览器、故障、迁移和 Compose 验收。

## Course References

- [第 3 课总览](https://devops.hello1023.com/课件/第3课-课件-认证授权与知识库入库/index.html)
- [架构与选型](https://devops.hello1023.com/课件/第3课-课件-认证授权与知识库入库/pages/architecture.html)
- [范围与 Web 要求](https://devops.hello1023.com/课件/第3课-课件-认证授权与知识库入库/pages/scope.html)
- [验收与提交](https://devops.hello1023.com/课件/第3课-课件-认证授权与知识库入库/pages/ship.html)

课件以本人规约为验收依据。技术选型与完整产品行为按本次决定统一；教程里的可替代实现或不一致表述按 design 的明确决策解释，不构成额外隐含要求。
