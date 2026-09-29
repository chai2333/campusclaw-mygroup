# Spec Delta

## Purpose

定义 CampusClaw 教师与学生的认证、会话、角色授权、运行配置、迁移和交付行为，使用户身份始终来自可信服务端，并能在单机容器环境中独立复现成功、失败和依赖故障场景。

## ADDED Requirements

### Requirement: 教师和学生可以登录
系统 SHALL 接受 JSON `POST /api/login` 的 username/password，仅支持 teacher/student 角色，成功返回 200 并建立新服务端会话。

#### Scenario: 教师登录
- **WHEN** 预置教师提交有效 JSON 凭据
- **THEN** 返回 200 与最小成功对象，并设置新的会话 Cookie。

#### Scenario: 学生登录
- **WHEN** 预置学生提交有效 JSON 凭据
- **THEN** 返回 200，后续身份查询显示 student 与正确班级。

#### Scenario: 非法登录请求
- **WHEN** 请求不是 JSON、字段缺失、字段不是非空字符串或 JSON 无效
- **THEN** 返回 400，不创建会话，不回显凭据。

#### Scenario: 传统表单登录不可用
- **WHEN** 客户端向 /login 提交传统凭据表单
- **THEN** 不建立认证会话；登录页面仅调用 JSON API。

### Requirement: 登录失败防枚举与限流
系统 MUST 按用户名与可信来源 IP 组合限制登录失败；未知账号、错误密码和锁定状态返回相同 401 JSON 文案，且执行等价成本的密码比较，不通过限流状态码泄露账号状态。

#### Scenario: 三类失败同形
- **WHEN** 分别请求不存在账号、错误密码和被锁定账号
- **THEN** 状态码、响应结构和文案一致，不创建会话；不存在及锁定分支也执行虚拟哈希比较。

#### Scenario: 达到失败阈值
- **WHEN** 同一用户名/IP 在配置窗口内达到失败次数阈值
- **THEN** 该组合进入配置锁定期，锁定期内正确密码也返回同形 401。

#### Scenario: 锁定到期恢复
- **WHEN** 锁定期结束后提交有效凭据
- **THEN** 允许登录并重置对应失败计数。

#### Scenario: 无法伪造来源绕过
- **WHEN** 客户端修改转发 IP 头后继续请求同一账号
- **THEN** 不可信来源声明不能替换用于限流的实际客户端来源。

#### Scenario: 独立组合与资源边界
- **WHEN** 不同账号/IP 组合发起请求且旧计数过期
- **THEN** 组合独立计数，旧状态被清理，限流状态存储有容量边界。

### Requirement: 会话生命周期与最小 Cookie
系统 MUST 创建不可预测的高熵不透明 ID；服务端保存用户关联、创建及过期时间，不能复制角色或班级；Cookie MUST 为 HttpOnly、SameSite=Lax，非开发环境 Secure。

#### Scenario: 会话随机且不含权限
- **WHEN** 同一账号连续登录并检查 Cookie 与服务端会话记录
- **THEN** ID 不同，Cookie 不含用户 ID/角色/班级/密码/密钥；会话仅关联用户与生命周期。

#### Scenario: 登录轮换
- **WHEN** 携带旧会话的客户端成功登录
- **THEN** 新 ID 生效，旧 ID 访问受保护 API 返回 401。

#### Scenario: 会话过期
- **WHEN** 过期 ID 调用 /api/me 或材料 API
- **THEN** 返回 401 且不返回业务数据；过期记录可被清理。

#### Scenario: 环境 Cookie 属性
- **WHEN** 分别在本地 HTTP 开发环境与非开发环境登录
- **THEN** 均包含 HttpOnly、SameSite=Lax；开发 Cookie 可在 HTTP 使用，非开发 Cookie 包含 Secure。

### Requirement: 客户端可以查询当前登录身份
系统 SHALL 提供 GET /api/me，身份、角色和 class_id MUST 从会话关联的当前用户记录加载。

#### Scenario: 查询有效身份
- **WHEN** 有效会话查询 /api/me
- **THEN** 返回 200 与 id、username、role、class_id，不含密码、哈希或密钥。

#### Scenario: 无效会话
- **WHEN** 缺失、伪造、撤销或过期 Cookie 查询 /api/me
- **THEN** 返回 JSON 401，无身份或班级信息。

#### Scenario: 权限即时更新
- **WHEN** 服务端修改用户角色或班级后发起下一次认证请求
- **THEN** 使用新角色与班级，客户端伪造声明不能覆盖。

#### Scenario: 无效用户
- **WHEN** 会话关联的用户已删除或具有不支持的角色
- **THEN** 拒绝认证/授权，不授予任何教师能力。

### Requirement: 用户可以注销服务端会话
系统 SHALL 提供 POST /api/logout，立即撤销会话并清 Cookie；无有效会话返回 401 并清 Cookie。

#### Scenario: 注销并复用旧 ID
- **WHEN** 有效会话注销后再次携带原 ID 查询身份或材料
- **THEN** 注销返回 200，原 ID 后续返回 401，不能恢复。

#### Scenario: 无有效会话注销
- **WHEN** 未登录或过期客户端调用注销
- **THEN** 返回 401 并清 Cookie，不创建新会话。

### Requirement: 页面与 API 认证语义分离
系统 MUST 将未登录受保护页面导航到 /login，受保护 API 返回 JSON 401；有效学生的教师操作返回 403。

#### Scenario: 未登录页面
- **WHEN** 无有效会话访问 /materials 或材料详情页面
- **THEN** 进入登录页且不返回任何业务内容。

#### Scenario: 未登录 API
- **WHEN** 无有效会话请求列表、详情、下载或上传
- **THEN** 返回 JSON 401，不含标题、正文或文件路径。

#### Scenario: 学生伪造角色
- **WHEN** 学生修改 query/Header/JSON/multipart/Cookie 中的角色后上传
- **THEN** 服务端仍返回 403，数据库和文件均不变。

### Requirement: 安全哈希与配置外置
系统 MUST 仅持久化加盐慢密码哈希，秘密仅从服务端环境读取；缺少必需值必须启动失败，真实值不得进入仓库、客户端、镜像或日志。

#### Scenario: 密码哈希
- **WHEN** 初始化相同密码的不同账号
- **THEN** 哈希不同，正确密码通过、错误密码失败，无明文或可逆密码。

#### Scenario: 缺少必需配置
- **WHEN** 缺少会话密钥、数据库凭据或种子密码之一
- **THEN** 服务拒绝启动，不回退默认秘密，不泄露其他变量值。

#### Scenario: 配置合法性
- **WHEN** TTL、上传限制或限流数值非法
- **THEN** 启动失败并只报告变量名及规则。

#### Scenario: 检查秘密泄漏
- **WHEN** 检查代码历史、构建上下文、镜像、日志、数据库及 HTTP 响应
- **THEN** 无真实密码/密钥泄漏；会话原始 ID 不进入日志；.env 与数据排除提交和构建。

### Requirement: 开发环境提供可区分且幂等的种子
系统 SHALL 提供 A/B 两班、各班教师和学生及各班至少一份可区分标题的材料与知识正文，密码来源于环境；初始化 MUST 幂等。

#### Scenario: 初始化空数据库
- **WHEN** 首次启动开发或测试环境
- **THEN** 创建四个 a-/b- 教师学生账号与双班示例文件、材料、知识条目，归属一致且只有哈希密码。

#### Scenario: 重复初始化
- **WHEN** 再次启动且已有用户与上传材料
- **THEN** 不复制种子，不覆盖旧密码、材料和正文。

#### Scenario: 种子用于隔离验证
- **WHEN** A 班用户搜索 B 班种子标题
- **THEN** 无匹配；B 班可查看对应材料与正文。

### Requirement: 存活与就绪分离
系统 SHALL 提供公共 GET /health 表示 HTTP 进程存活，以及 GET /ready 检查数据库与上传目录；依赖故障 MUST 不被判为认证失败。

#### Scenario: 公共存活
- **WHEN** 未登录访问 /health 且 HTTP 处理可用
- **THEN** 返回 200 最小状态，不跳登录、不查询数据库。

#### Scenario: 正常就绪
- **WHEN** 数据库与上传目录可用时请求 /ready
- **THEN** 返回 200 最小状态。

#### Scenario: 数据库故障
- **WHEN** 数据库不可用但 HTTP 处理可用，持有会话请求依赖数据库的接口
- **THEN** /health 返回 200，/ready 和该业务请求返回 503，不清 Cookie、不改报 401、不暴露内部错误。

#### Scenario: 上传目录故障
- **WHEN** 上传目录不可用时请求 /ready
- **THEN** 返回 503，不泄露路径，存活检查仍可返回 200。

### Requirement: Compose 提供单实例标准部署
系统 MUST 提供 web/api/db 三服务的一条 Compose 构建启动命令，仅 web 发布宿主端口；数据库与原文件持久化，API 使用普通数据库账号并有有界就绪重试。

#### Scenario: 从零启动
- **WHEN** 第三方按 .env.example 和 README 配置后执行 docker compose up --build -d
- **THEN** 三服务可用，可经 web 登录与访问探针；默认入口 localhost:8080，端口可配置。

#### Scenario: 网络和权限
- **WHEN** 检查标准部署端口、挂载与运行身份
- **THEN** api/db 无宿主端口，上传卷仅 API 挂载，API/Web 非 root，数据库管理员凭据不进入 API。

#### Scenario: 数据库延迟
- **WHEN** 数据库初始化延迟但在重试期限内就绪
- **THEN** API 重试后可用；超出期限则脱敏失败，不无限等待。

#### Scenario: 容器重建持久化
- **WHEN** 写入账号和材料后执行 down 再 up，不使用 -v
- **THEN** 用户、班级、材料、正文和文件保持完整。

### Requirement: 已有数据迁移不丢失
系统 MUST 提供旧数据备份、显式迁移和切换前回滚流程，保留业务身份、密码可用性、班级及材料正文与文件；旧会话必须失效。

#### Scenario: 迁移正常数据
- **WHEN** 将既有 SQLite 数据和上传文件导入目标库与新卷
- **THEN** 账号/班级/ready 材料 ID、归属、时间、正文顺序和文件校验和匹配；旧密码可登录，无明文密码导出。

#### Scenario: 异常旧记录
- **WHEN** 源数据含非 ready 材料、缺失原文件或归属冲突
- **THEN** 非 ready 记录保存在源备份和异常清单；ready 数据异常中止切换，不静默删除或发布不完整数据。

#### Scenario: 重跑与会话
- **WHEN** 重复导入相同源数据并尝试旧会话
- **THEN** 不复制数据，相冲突的目标记录阻止继续；旧会话返回 401，用户重新登录。

#### Scenario: 切换前回滚
- **WHEN** 目标验证失败且未放行新业务写入
- **THEN** 能用只读保留的原数据库/文件与旧版本恢复服务，不删除源卷。

### Requirement: 交付证据可独立复核
系统交付 MUST 包括 README、与实现一致的 ADR 和逐场景验收记录；完整课程交付须留下同伴互验与版本定位证据，之后同步并归档规约。

#### Scenario: 复现与文档
- **WHEN** 第三方在干净环境只阅读 README
- **THEN** 可完成配置、启动、登录、上传、读取、下载与失败验证，能理解范围、不做项、单实例和跨班返回码。

#### Scenario: 记录验收
- **WHEN** 完成新栈验证
- **THEN** 每个 Scenario 有命令/截图/测试输出及通过或失败结论；未执行不能标完成，至少覆盖未登录、学生上传、跨班三类负向证据。

#### Scenario: 课程收尾
- **WHEN** 完成实施并由同伴验证跨班 URL 后归档
- **THEN** 记录互验与版本/提交信息；归档后活动变更移入带日期目录，主规格与 delta 一致；作业提交留用户执行记录。
