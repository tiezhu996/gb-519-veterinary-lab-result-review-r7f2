
# 兽医检验样本结果复核

动物样本来源、样本接收、检测运行和结果签发平台。项目采用前后端分离和明确的领域分层，重点保证状态迁移、RBAC、审计日志、请求追踪与限流在各层保持一致。

## Docker Compose 快速启动

```bash
cp .env.example .env
docker compose up -d --build
docker compose ps
```

- Web 工作台：http://127.0.0.1:18519
- 后端健康检查：http://127.0.0.1:19519/healthz
- 后端 API：http://127.0.0.1:19519/api
- 演示账号：`admin`、`reviewer`、`operator`、`viewer`
- 演示密码：`Admin123!`（仅限本地演示，生产环境必须更换）

停止并清理本项目容器与数据卷：

```bash
docker compose down -v --remove-orphans
```

## 主要功能

| 业务模块 | 后端实体 | API 前缀 | 状态流 |
|---|---|---|---|
| 动物样本来源 | `AnimalCase` | `/api/cases` | registered, sampling, testing, closed |
| 检验样本 | `Specimen` | `/api/specimens` | received, testing, hold, released, disposed |
| 检测运行 | `AssayRun` | `/api/assays` | planned, running, validated, invalid |
| 结果签发 | `ResultSignoff` | `/api/signoff` | draft, peer_review, signed, rejected |
| 危急检验结果处置闸门 | `CriticalDisposition` | `/api/dispositions` | pending, confirmed, void |

- 独立登录页和 viewer/operator/reviewer/admin 四级 RBAC；写操作至少需要 operator，审计接口至少需要 reviewer。
- 结果签发只能按 `draft -> peer_review -> signed/rejected` 推进；签发与驳回必须由不同于制单人的 reviewer/admin 完成。
- **危急检验结果处置闸门**：检测运行核验通过（`validated`）后，风险级别为“严重”（`critical`）的运行必须自动生成待处置事项（`pending`）。
- 使用同一业务关联编号（`relatedCode`）的严重风险结果，在处置事项确认前只能保留草稿，提交复核会被拒绝（422）。
- 处置确认人须为 reviewer/admin，且不能是检测运行操作员（`operatedBy`）；确认时必须填写接收对象与处置措施。
- 同一事项的重复或并发确认只会成功一次（条件更新 + 乐观锁，失败者返回 409）。
- 检测运行随后变为 `invalid` 时，原处置确认（或待处置）作废为 `void`，并再次阻断同一关联编号的结果；运行重新核验通过会补登新的待处置事项。
- 检测运行页与结果签发页均显示处置状态、检测运行操作员、接收对象、处置措施、确认人与关联运行，刷新后从接口回读；普通结果与既有异人复核规则不受影响。
- 每次签发创建、草稿编辑和状态决策都会追加不可覆盖的版本，保留证据、操作者、原因和 request ID。
- 所有状态变化使用乐观锁并写入不可覆盖的审计日志；已进入复核的签发业务字段不可再编辑。
- 请求 ID、结构化日志、全局错误映射和 Redis 分布式限流。
- 提供脱敏运行配置、当前会话、审计汇总和单实体审计历史接口。
- 业务工作台支持查询、新建、状态推进、风险标识及操作审计查看。

## 技术栈

| 层次 | 技术 |
|---|---|
| 前端 | React 18 + TypeScript + Vite + Material UI |
| 后端 | Go 1.22 + Gin + GORM |
| 数据 | MySQL + Redis |
| 部署 | Docker Compose + Nginx |

## 本地开发

后端可使用 SQLite 开发模式，不需要先启动数据库：

```bash
cd backend
go mod download
DATABASE_DRIVER=sqlite DATABASE_DSN=local.db REDIS_ADDR='' \
JWT_SECRET=local-development-secret PORT=8080 go run ./cmd/server
```

前端开发服务器：

```bash
cd frontend
npm install
npm run dev
```

质量检查：

```bash
cd backend && go test ./... && go build ./...
cd ../frontend && npm run typecheck && npm run build
cd .. && docker compose config --quiet
```

也可以从项目根目录执行 `./scripts/validate.sh`。脚本会运行 Go 单测/race/vet/build、前端类型检查和构建，从空卷启动 Compose，验证四级 RBAC、异人签发和完整版本证据，并在成功或失败时自动关闭容器。需要暂留服务给内置 Browser 验收时使用：

```bash
KEEP_RUNNING=1 ./scripts/validate.sh
```

## 目录结构

```text
.
├── backend/
│   ├── cmd/server/                 # 服务入口与优雅退出
│   └── internal/
│       ├── config/                 # 环境配置
│       ├── constants/              # 状态枚举与迁移图
│       ├── database/               # 连接、迁移与演示数据
│       ├── dto/                    # 输入契约
│       ├── handler/                # HTTP 接口
│       ├── middleware/             # JWT、追踪、限流
│       ├── model/                  # GORM 实体与签发修订链
│       ├── repository/             # 持久化边界
│       ├── router/                 # 路由装配
│       ├── service/                # 业务规则与审计
│       └── util/                   # 统一 HTTP 响应
├── frontend/src/
│   ├── api/                        # 按实体拆分的 API
│   ├── components/common/          # 共享业务组件
│   ├── hooks/                      # 认证与分页 hooks
│   ├── pages/                      # 五个路由页面
│   ├── router/                     # 路由配置
│   ├── stores/                     # 按实体拆分的状态仓库
│   ├── types/                      # 共享类型与枚举
│   └── utils/                      # 格式化与状态工具
├── docker-compose.yml
└── runtime_smoke.json
```

## 共享枚举位置

| 枚举 | 值 | 前后端出现位置 |
|---|---|---|
| `SpecimenState` | `received, testing, hold, released, disposed` | `backend/internal/constants/status.go`、`frontend/src/types/status.ts` |
| `SignoffState` | `draft, peer_review, signed, rejected` | `backend/internal/constants/status.go`、`frontend/src/types/status.ts` |
| `DispositionState` | `pending, confirmed, void` | `backend/internal/constants/status.go`、`frontend/src/types/status.ts` |

每个实体自己的完整迁移图同样位于 `backend/internal/constants/status.go`；页面使用的状态列表位于 `frontend/src/types/status.ts`。修改状态时必须同步两处并更新对应服务测试。

## 结果签发控制

1. operator 创建签发草稿，系统记录 `preparedBy` 并生成 v1。
2. 只有原制单人可以编辑或提交草稿；每次编辑和提交均追加版本。
3. operator 不能作出最终签发决定；reviewer/admin 可以签发或驳回，但操作者必须不同于 `preparedBy`。
4. `signed` 和 `rejected` 为终态，全部修订可从签发查询接口读取，审计历史可由 reviewer/admin 查询。

## 危急检验结果处置闸门

1. operator 将严重（`critical`）检测运行推进到 `validated`，系统在同一事务内创建 `CriticalDisposition(pending)`，并快照检测运行操作员 `operatedBy`。
2. 同一 `relatedCode` 的严重结果提交 `peer_review` 前，系统读取最新闸门：仅 `confirmed` 放行；`pending` 或运行失效后的 `void` 一律阻断。
3. reviewer/admin（且不等于 `operatedBy`）调用 `POST /api/dispositions/:id/confirm`，提交 `recipient` 与 `measure`；重复/并发确认最多成功一次。
4. 运行被置为 `invalid` 时，其未失效事项在运行迁移事务内作废为 `void`；重新 `validated` 会补登新的 `pending` 事项，旧事项作为历史保留。
5. `/api/assays` 返回每个运行的 `dispositions`；`/api/signoff` 返回每条结果的只读 `gate` 快照；处置事项可通过 `/api/dispositions` 单独查询。

## 环境变量

| 变量 | 说明 |
|---|---|
| `COMPOSE_PROJECT_NAME` | 固定英文 Compose 项目名，支持中文父目录 |
| `DB_NAME/DB_USER/DB_PASSWORD` | 数据库名称与业务账号 |
| `DB_ROOT_PASSWORD` | MySQL 管理员密码（PostgreSQL 项目保留统一模板字段） |
| `JWT_SECRET` | JWT 签名密钥，生产环境必须替换 |
| `FRONTEND_PORT/BACKEND_PORT/DB_PORT` | 宿主机端口映射 |
| `REDIS_PORT` | Redis 宿主机端口 |
| `MINIO_*` | 证据对象存储配置（启用 MinIO 的项目） |

## API 使用示例

```bash
token=$(curl -sS -X POST http://127.0.0.1:19519/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"Admin123!"}' | jq -r '.data.token')

curl -sS http://127.0.0.1:19519/api/overview \
  -H "Authorization: Bearer $token"
```

## License

MIT
