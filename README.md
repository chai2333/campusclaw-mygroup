# CampusClaw

一个按班级隔离的课程材料知识库。教师上传 UTF-8 的 `.txt` 或 `.md` 材料，学生和教师可查看本班已入库材料。

技术栈：Go `net/http` + MySQL 8.0 + React 18/TypeScript/Vite + Nginx。

## 快速启动

```bash
cp .env.example .env
# 填写 .env 中的全部空秘密值
docker compose up --build -d
```

服务监听 `http://localhost:${WEB_PORT:-8080}`。

预置账号：`a-teacher`、`a-student`、`b-teacher`、`b-student`。密码来自 `.env` 中的 `SEED_TEACHER_PASSWORD` 和 `SEED_STUDENT_PASSWORD`。

## 配置

| 变量 | 说明 |
| --- | --- |
| `APP_ENV` | `development`（HTTP 不加 Secure）、`production` 或 `test` |
| `SESSION_SECRET` | 会话 HMAC 密钥，必填 |
| `DB_HOST` / `DB_PORT` / `DB_NAME` / `DB_USER` / `DB_PASSWORD` | MySQL 连接 |
| `MYSQL_ROOT_PASSWORD` | 仅 db 容器初始化使用，不下发 api |
| `UPLOAD_DIR` | 上传文件目录 |
| `MAX_UPLOAD_BYTES` | 单文件上限（默认 2097152） |
| `SESSION_TTL_SECONDS` | 会话有效期（默认 28800） |
| `SEED_TEACHER_PASSWORD` / `SEED_STUDENT_PASSWORD` | 种子账号密码 |
| `LOGIN_MAX_FAILURES` / `LOGIN_WINDOW_SECONDS` / `LOGIN_LOCK_SECONDS` | 限流参数 |
| `TRUSTED_PROXY_CIDRS` | 可信代理 CIDR |
| `WEB_PORT` | Nginx 发布端口（默认 8080） |

## 健康检查与就绪

- `GET /health`：存活检查，不验证依赖，始终 200。
- `GET /ready`：就绪检查，验证 MySQL 连接和上传目录可写；不就绪返回 503。

## API 接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/api/login` | JSON `{username, password}` → 200/401/400 |
| `GET` | `/api/me` | 当前用户信息 → 200/401 |
| `POST` | `/api/logout` | 注销 → 200/401 |
| `GET` | `/api/materials?q=` | 本班材料列表/搜索 → 200/401/403 |
| `POST` | `/api/materials` | 教师 multipart 上传 → 201/403/400/413 |
| `GET` | `/api/materials/{id}` | 本班材料详情 → 200/404 |
| `GET` | `/api/materials/{id}/file` | 本班文件下载 → 200/404 |

跨班/不存在资源统一返回 `404 {"error":"not_found"}`。数据库故障返回 503。错误响应不含密钥、正文、堆栈或磁盘路径。

## 从旧版迁移

旧版使用 Flask + SQLite + Argon2id。迁移步骤：

1. 停止旧服务写入。
2. 备份 SQLite 数据库和上传文件（`internal/migrate.Backup`）。
3. 启动新栈（`docker compose up --build -d`），新库自动迁移和补种子。
4. 运行迁移工具将旧数据导入新 MySQL：
   ```bash
   docker compose run --rm api /server migrate \
     --sqlite /path/to/campusclaw.sqlite3 \
     --uploads /path/to/instance/uploads
   ```
5. 验证：账号数、材料数、文件校验和一致，旧密码可登录。
6. 旧会话不迁移，切换后所有用户重新登录。

**回滚边界**：新栈开始写入前可恢复旧版本与原卷。若新栈已产生业务写入，不能直接切回旧 SQLite，必须先备份新数据并制定反向迁移。禁止将 `docker compose down -v` 当迁移步骤。

## 架构

```
浏览器 → Nginx (web) → Go API (api) → MySQL (db)
```

- 只有 web 发布宿主端口。
- API 使用普通 DB 账号，无管理员凭据。
- 上传卷仅 API 挂载。
- `/uploads` 不走静态或 SPA fallback。
- 单 API 实例，限流状态在进程内。

## 开发

后端测试（需要 MySQL）：
```bash
cd backend
docker run --rm --network host \
  -e TEST_MYSQL_DSN="campusclaw:pw@tcp(127.0.0.1:3306)/campusclaw?parseTime=true" \
  -v "$PWD:/src" -w /src golang:1.26.1-alpine \
  go test ./... -p 1
```

前端开发：
```bash
cd frontend
npm install
npm run dev   # Vite dev server with proxy to :8080
```

## 范围与不做项

- 单 API 实例，不引入 Redis、消息队列、向量库。
- 无公网基础设施，无独立管理后台。
- 无 PDF/Office 通用文件预览（仅 txt/md 正文展示）。
