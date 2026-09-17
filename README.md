# K12Edu Backend

面向 iPhone、iPad、Mac 客户端的 Go 云端服务。支持本地优先模式：客户端可以只使用本地模型和本地数据；登录云端后再启用同步、云端 AI 和云端存储能力。

## 已实现模块

- Gin API、PostgreSQL 数据底座、Redis 缓存与分布式限流
- 邮箱注册登录、JWT 刷新、Apple identity token 登录、账号注销、密码修改、数据导出
- 多设备注册、增量事件、版本保护、递归三方合并、数组去重
- AI Provider 网关、请求限流、月度配额、token/费用累计、敏感请求拦截、安全事件审计
- 用户、内容、AI 用量、安全审核、审计日志管理接口
- Vue 3 + Element Plus 管理后台：`/admin/`
- Docker Compose：PostgreSQL、Redis、API

## 关键环境变量

`K12EDU_DATABASE_URL`、`K12EDU_REDIS_URL`、`K12EDU_JWT_SECRET`、`K12EDU_AI_BASE_URL`、`K12EDU_AI_API_KEY`、`K12EDU_APPLE_CLIENT_ID`、`K12EDU_ADMIN_EMAIL`、`K12EDU_ADMIN_PASSWORD`。邮件找回使用 `K12EDU_SMTP_HOST`、`K12EDU_SMTP_PORT`、`K12EDU_SMTP_USER`、`K12EDU_SMTP_PASSWORD`、`K12EDU_SMTP_FROM`。文件存储使用 `K12EDU_STORAGE_DRIVER=local|s3`，S3 模式配置 `K12EDU_STORAGE_ENDPOINT`、`K12EDU_STORAGE_ACCESS_KEY`、`K12EDU_STORAGE_SECRET_KEY`、`K12EDU_STORAGE_BUCKET`、`K12EDU_STORAGE_REGION`、`K12EDU_STORAGE_SSL=true`。

## 主要接口

- `POST /api/v1/auth/apple`
- `POST /api/v1/sync/merge`
- `GET|PUT /api/v1/sync/progress`
- `POST /api/v1/ai/chat/completions`
- `GET /api/v1/admin/stats`
- `GET /api/v1/admin/ai-usage`
- `GET /api/v1/admin/ai-safety-events`
- `GET /api/v1/admin/audit-logs`

## 当前明确不包含

本阶段按要求不做自动化测试、部署、真实设备联调、App Store 上架流程和压力测试。上线前仍需补齐这些验证工作，并将管理后台的 CDN 依赖替换为正式的离线构建产物。
