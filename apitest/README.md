# 第5课接口验收

从仓库根目录运行 `python3 apitest/run_all.py --compose`。需要 Docker Compose；脚本自动生成只存本机的测试 `.env`，构建独立栈、跑完所有 URL 脚本、生成 HTML 报告后清理测试栈。添加 `--keep-stack` 可保留浏览器验收环境。

`cases/api/login/POST.py` 对应 `POST /api/login`，带 `{id}` 的目录对应 URL 路径参数。31个接口脚本覆盖新旧API；同一脚本内覆盖多个场景；共享 HTTP、数据库和 SSE 辅助函数放在 `common/`。新增接口脚本时也应更新 `run_all.py` 的执行清单；执行器会检查是否遗漏脚本。

测试准备两个独立班级、教师、同班两个学生以及异班学生。相关响应字段与实际 MySQL 记录核对，并按发布的 OpenAPI Schema 校验；SSE 从实际 Nginx 代理逐字节读取。完成后清理本次生成的数据库记录，上传文件随独立测试卷清理。

报告在 `reports/report.html`，JSON 结果在 `reports/results.json`；全部不提交 Git，失败/启动失败/清理失败均返回非零退出码。不要将测试运行器指向业务数据库；数据库名必须以 `_apitest` 结尾。

如果连接自行准备的测试栈，安装 `requirements.txt` 后运行 `python3 apitest/run_all.py`。本机 `.env` 需配置 `APITEST_BASE_URL`、`APITEST_DB_HOST`、`APITEST_DB_PORT`、`APITEST_DB_NAME`、`APITEST_DB_USER`、`APITEST_DB_PASSWORD`，以及与目标测试 API 一致的 `SESSION_SECRET`、`SEED_TEACHER_PASSWORD`、`SEED_STUDENT_PASSWORD`。数据库需先由应用迁移并初始化种子账号。

`APITEST_PROVIDER=mock` 仅用于随仓库提供的模拟服务；真实模型环境应为 `real`。模拟服务会故意响应失败、违规答案和非法引用以覆盖终止状态，这些触发器仅存在于独立测试服务，不进入产品 API。

新版消息协议为meta → delta → done/error。meta先返回技能与候选引用；会话创建必须选择agent_id。专项真实数学/语文与注入验收见accept_real.py，回答证据写入reports/real-model.json。
