# Spec Delta

## ADDED Requirements

### Requirement: 用户私有且固定智能体的会话
教师和学生 MUST 均可创建和读取自己的本班会话；创建必须选择本班已启用agent_id，无可用智能体400，未知/停用/他班智能体404。会话之后固定该智能体，请求额外编号不得更换。该智能体停用后已有会话仍可用。他人/他班/不存在会话同形404。新标题为新的提问，首轮取前24字。

#### Scenario: 会话固定与停用
- **WHEN** 会话选定A，教师停用A或消息请求携带B
- **THEN** 已有会话继续用A，不能新选停用A，也不能换成B

### Requirement: 有界历史和问题验证
问题去首尾空白后 MUST 为2–500 Unicode字，非法正文/尾随JSON/未认证在SSE前拒绝。上下文 MUST 取同一会话末尾6条已完成非空user/assistant消息（含本轮问题），每条截断500字，按顺序组装；全量历史仍保存。失败回答不得被标为完成。

#### Scenario: 长会话只送末尾窗口
- **WHEN** 会话已有超过6条消息又提交新问题
- **THEN** 模型只收到末尾6条各至多500字，旧历史仍可查看，当前问题不重复

### Requirement: meta先行的POST SSE
受保护消息接口 MUST 使用Bearer POST fetch，返回UTF-8 SSE、禁用代理缓冲并每帧flush。成功顺序为一个meta、多个新增delta、一个提交后done；meta包含status:ok、技能或null、完整候选引用和消息标识。失败前置JSON，meta后只能error且无done。检索失败 MUST 在meta前JSON503且不调用模型。

#### Scenario: 引用先于正文到达
- **WHEN** 检索完成、模型尚未开始
- **THEN** 浏览器先收到meta并显示技能/出处，随后delta逐段渲染，done才标记完成

#### Scenario: 并发与主动取消
- **WHEN** 同一会话生成中再次发送或主动断开
- **THEN** 重发409无重复消息，断开取消下游并保存cancelled、清锁，下一轮可继续

### Requirement: 注解生成文档和接口一致性验收
系统 MUST 从处理器源码注解生成OpenAPI并提供本地Swagger试调，所有受保护接口标明Bearer/角色/错误。apitest MUST 按URL/HTTP方法一接口一脚本，一键生成Git忽略的HTML报告，核对响应与MySQL并覆盖新智能体/技能/绑定/会话/SSE。日志报告镜像不得含秘密。

#### Scenario: 生成产物可复现
- **WHEN** 修改处理器注解后执行生成命令或Docker构建
- **THEN** OpenAPI同步更新、覆盖所有已注册接口，Swagger能实际试调
