# k12edu-backend

`k12edu` 的 Go 云端服务，负责账号、云端数据同步、平台 AI 服务和 Web 管理后台。

## 当前状态

仓库从空仓库开始搭建。当前已提供：

- Go + Gin API 入口
- `/healthz` 和 `/api/v1/health` 健康检查
- `/admin/` 管理后台页面壳

管理员账号可通过 `K12EDU_ADMIN_EMAIL` 和 `K12EDU_ADMIN_PASSWORD` 在启动时初始化。开发环境可运行 `docker compose up --build`，然后访问 `/admin/`。
- Dockerfile 和 Docker Compose

## 本地运行

```bash
go run ./cmd/api
```

打开：

- http://localhost:8080/healthz
- http://localhost:8080/admin/

## 规划

1. PostgreSQL 数据模型与迁移
2. 用户认证、Apple 登录和账号删除
3. 本地/云端同步 API
4. AI Provider 网关与用量限制
5. Vue 3 管理后台替换当前页面壳
