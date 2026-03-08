# Compose UI

Compose UI 是一个面向单机 Docker 环境的 Compose 可视化管理工具。  
它提供 Compose 项目管理、容器管理、镜像管理和日志查看能力，支持前后端一体化部署。

## 功能特性

### Compose 项目管理

- 自动发现宿主机 Compose 项目，并按项目聚合展示服务
- 支持读取、编辑、格式化并保存 Compose 文件（带 `mtime` 乐观锁）
- 支持项目级操作：启动、停止、重部署

### 容器管理

- 支持全局容器列表查看（容器名、所属项目、镜像、状态）
- 支持按关键字搜索容器（容器名/镜像/项目）
- 支持容器操作：重启、停止、删除
- 支持容器日志查看（历史日志 + SSE 实时日志）

### 镜像管理

- 支持列出所有镜像并按关键字搜索
- 支持按是否使用过滤（使用中/未使用）
- 支持批量删除、删除所有空镜像、删除所有未使用镜像

## 技术栈

- 后端：Go
- 前端：Vue 3 + TypeScript + Vite + Monaco Editor
- 运行环境：Docker Engine + Docker Compose Plugin

## 项目结构

- `compose-ui/`：Go 后端 API 服务
- `compose-ui-web/`：Vue3 前端工程
- `deploy.sh`：一体化构建脚本（前端构建 + 后端多架构打包）

## 快速开始

### 依赖

- Docker Engine
- Docker Compose Plugin（`docker compose`）
- Go（支持 toolchain 自动下载）
- Node.js 18+

### 本地开发

后端内嵌 `compose-ui/internal/api/webui/dist` 静态资源。  
全新拉取代码后建议先执行一次 `./deploy.sh`，或手动构建前端并同步到该目录。

后端启动：

```bash
cd compose-ui
go run ./cmd/server
```

前端启动（开发模式）：

```bash
cd compose-ui-web
npm install
npm run dev
```

前端默认请求同源 `/api/v1`，可通过 `VITE_API_BASE` 覆盖（如 `http://127.0.0.1:8227/api/v1`）。

### 一体化部署打包（Linux amd64/arm64）

在仓库根目录执行：

```bash
./deploy.sh
```

脚本会自动：

- 构建前端并嵌入后端（`compose-ui/internal/api/webui/dist`）
- 构建 Linux `amd64` 和 `arm64` 后端二进制
- 生成发布包到 `release/`

## 配置项

可通过环境变量配置后端：

- `COMPOSE_UI_ADDR`：监听地址，默认 `:8227`
- `COMPOSE_UI_REDEPLOY_TIMEOUT`：重部署超时，默认 `120s`
- `COMPOSE_UI_BASIC_AUTH_USER`：BasicAuth 用户名，默认 `admin`
- `COMPOSE_UI_BASIC_AUTH_PASS`：BasicAuth 密码，默认 `admin`

## API

- `GET /api/v1/projects`
- `GET /api/v1/containers?keyword=...`
- `GET /api/v1/projects/:projectId/compose-file`
- `PUT /api/v1/projects/:projectId/compose-file`
- `POST /api/v1/projects/:projectId/redeploy`
- `POST /api/v1/projects/redeploy-by-image`
- `POST /api/v1/services/:serviceId/action`
- `POST /api/v1/projects/:projectId/action`
- `GET /api/v1/projects/:projectId/action-stream?action=start|stop|redeploy`
- `GET /api/v1/images?keyword=...&used=used|unused`
- `POST /api/v1/images/delete`
- `GET /api/v1/containers/:containerId/logs?tail=200&follow=false`
- `GET /api/v1/containers/:containerId/logs/stream`

## 关键实现策略

- Compose 文件定位优先使用 Docker label：
  - `com.docker.compose.project.working_dir`
  - `com.docker.compose.project.config_files`
- Label 缺失时降级使用挂载目录推断 Compose 文件
- 保存 Compose 文件时：
  - 使用 `expectedMtime` 做乐观锁
  - 自动创建 `*.bak.<timestamp>` 备份
- 重部署命令：`docker compose -f <file> up -d`

### `POST /api/v1/projects/redeploy-by-image`

按项目名更新指定 service 的 `image` 字段并立即重部署。

请求体示例：

```json
{
  "projectName": "demo",
  "serviceName": "web",
  "image": "nginx:1.27.5"
}
```

典型错误：

- `PROJECT_NOT_FOUND`：项目名未匹配到 Compose 项目
- `PROJECT_AMBIGUOUS`：存在多个同名项目
- `COMPOSE_NOT_EDITABLE`：项目未关联可编辑 compose 文件
- `INVALID_COMPOSE`：compose 文件 YAML 非法
- `SERVICE_NOT_FOUND`：compose 中不存在目标 service
- `SERVICE_IMAGE_INVALID`：目标 service 的 `image` 字段缺失或不是字符串

## 限制说明

- 仅支持单机 Docker Host
- 当前使用 BasicAuth，建议部署到受信任网络并修改默认账号密码
- 未关联 Compose 文件的项目仅支持基础管理与日志查看
