# mgame

Go 分布式游戏服务器骨架：**gate（网关）+ game（游戏服）双服务**，支持 etcd 服务注册发现、JWT 客户端鉴权、gate↔game HMAC 内网握手。

- 模块路径：`github.com/zhenhuasu/mgame`
- 单 Go 模块，`go build ./...` 即可运行；**用户/角色持久化在 MongoDB（users/roles 两集合），Redis 只做读缓存与全局 roleId 自增**，两者任一不可用 = 启动失败

## 目录结构

```
mgame/
├── core/                # 跨服务公共能力
│   ├── net/             # cellnet 装配、事件队列、统一消息分发、注册中心托管
│   ├── svcproc/         # gate↔game 后端自定义 proc（LTV 帧）
│   ├── auth/            # gate↔game HMAC-SHA256 签名/校验
│   ├── registry/        # 服务注册与发现（etcd / 内存静态实现）
│   ├── redis/           # Redis 连接与 get/set/del/incr（fail-fast）
│   └── mongo/           # MongoDB 连接 + 泛型 CRUD/索引助手（fail-fast）
├── gate/                # 网关服务：ws/wss 接入客户端，tcp 连 game
│   ├── main.go          # 入口：配置 + net.Init
│   ├── app.go           # 装配：前端/后端/登录态接线 + 自注册 + 事件循环
│   ├── frontend/        # 与客户端通信：ws 装配、客户端 handler、token 鉴权
│   ├── backend/         # 与 game 通信：连接池、实例发现、内网 HMAC 握手
│   └── session/         # 共享登录态（顶号 / 过期 / 广播）
├── game/                # 游戏服务：用户/角色、在线管理、业务
│   ├── main.go          # 入口：配置 + net.Init + Mongo/Redis/仓储/管理器装配
│   ├── handler/         # 消息处理：gate 握手、登录/生命周期、角色进入、业务转发
│   ├── repo/            # 仓储：Mongo 两集合 + Redis 读缓存装饰器 + 唯一索引
│   ├── service/         # 业务：UserManager（在线/角色/进入游戏）、RoleIDGen（Redis INCR）
│   ├── model/           # 领域结构（User / Role）
│   └── network/         # 传输装配（cellnet Listen + 自注册）
├── client/              # 测试客户端（Ping / 登录 / 广播 / Hello）
├── proto/               # protobuf（gate.proto 客户端协议 + inner.proto 内网协议）
├── config/              # 配置加载与各服务 yaml
└── AGENTS.md            # 项目约定与实现细节
```

## 快速开始

需要 Go 1.26+、本机 MongoDB（27017）与 Redis（6379）——任一连不上 game 直接启动失败。

```bash
go build ./...
go vet ./...

# 1. 起游戏服（默认 127.0.0.1:8901；需本机 MongoDB + Redis）
go run ./game -config config/game.yaml
# 第二个实例（负载均衡用，127.0.0.1:8902）
go run ./game -config config/game2.yaml

# 2. 起网关（ws://0.0.0.0:8801/ws）
go run ./gate -config config/gate.yaml

# 3. 测试客户端
go run ./client -addr ws://127.0.0.1:8801/ws                          # Ping
go run ./client -account a -uid 1001 -token t -version v1.0.0          # 登录（打印角色）+ 自动建角色并进入
go run ./client -account a -uid 1001 -token t -flow enter -role 3      # 直接进入指定角色
go run ./client -account a -uid 1001 -token t -flow create -role-name b # 强制新建角色并进入
go run ./client -account a -uid 1001 -token t -broadcast -content hi   # 全局广播
go run ./client -account a -uid 1001 -token t -hello -content hi       # Hello 经 gate→game→gate 回包

mongosh mgame --quiet --eval 'db.roles.find().toArray()'               # 角色集合
redis-cli get mgame:user:a                                             # 用户读缓存（登录/写后失效）
redis-cli get mgame:role:id                                            # 全局角色 ID 计数器
```

> 构建二进制请输出到 `bin/`（`go build -o bin/ ./gate ./game`），`/bin/` 已被 gitignore。

## 配置

各服务配置在 `config/*.yaml`：

| 字段 | 说明 |
|---|---|
| `listen` / `path` / `protocol` | 监听地址与协议（`ws` / `wss`） |
| `auth.mode` | `dev`（占位校验）或 `jwt`（HS256 本地验签，需 `jwtSecret`） |
| `auth.tokenTTL` | 登录会话有效期（秒） |
| `gateSecret` | gate↔game 内网 HMAC 共享密钥，空 = 不校验（仅联调） |
| `advertise` | 注册到 etcd 的对外地址，空 = 回退 `listen` |
| `registry.endpoints` | etcd 地址，**非空即启用**；空 = 不启用，gate 回退静态 `gameServers` |
| `registry.prefix` | etcd 键前缀，空 = `/mgame` |
| `registry.ttl` | 租约秒数，默认 10s |
| `mongo.uri` | MongoDB 连接串，**空 = game 启动失败** |
| `mongo.database` | 数据库名，空 = `mgame` |
| `redis.addr` | 各服务共用的 Redis 地址，**空 = game 启动失败** |
| `redis.password` / `redis.db` | Redis 密码与库序号 |
| `user.prefix` | game 用户读缓存键前缀，空 = `mgame:user:` |
| `user.ttl` | 读缓存过期秒数，0 = 不过期 |
| `user.autoCreateRole` | 角色列表为空时，允许 `EnterGame(role_id=0)` 由服务端默认建角 |

wss 示例见 `config/gate.wss.yaml`，证书用 `certs/`（已被 gitignore，需自行用 openssl 生成）。

## 登录、角色与进入游戏

1. 客户端发 `LoginReq{account,uid,token,version}` → gate `VerifyToken` 校验 → `session.Manager` 登录（顶号踢旧连接）→ RoundRobin 分配 game → 发 `PlayerLogin{uid,account,ip,version}`（ip 为 gate 采集的客户端出口地址）。
2. game `UserManager.Login`：Mongo `users` 幂等 upsert（刷新 uid/ip/version）→ 账号有角色时按 `roles.account` 加载角色基本信息 → 登记在线表；缓存装饰器随后失效该账号缓存。
3. game 回 `PlayerLoginAck{code,roles}` → gate `onPlayerLoginAck` 回投 `LoginAck{code,reason,uid,roles}`；失败则回滚路由与登录态。**新账号此时 roles 为空，登录不创建角色**。
4. 无 game 可用时 gate 直接回 `LoginAck{code:1, reason:"no game available"}`（快速失败，不等待）。
5. 客户端随后走进入流程：
   - `CreateRoleReq{name}` → `RoleIDGen` 从 Redis `mgame:role:id` **INCR** 取全局 ID（单命令原子，**无需分布式锁**，失败只留空洞）→ 插 `roles` 集合（**角色名全局唯一**，重名被唯一索引拦截，回 `code=1 reason=role name already exists`）→ `CreateRoleAck{role}`；
   - `EnterGameReq{role_id}` → 按 `account` 加载角色**完整数据**校验归属 → **仅内存**置 `OnlineRole` → `EnterGameAck{role}`（不写库）。`role_id=0` 仅当 `user.autoCreateRole=true` 且账号无角色时自动建默认角色，否则报 `role_id required`。
6. 下线/退出：`PlayerOffline` 仅清理内存与在线表（**不写库**）；gate 连接断开只摘在线记录；gate 重连后 `PlayerOnline` 从仓储恢复（`OnlineRole` 不落库故为 nil，需客户端重新 `EnterGame`）。

### 持久化结构（MongoDB `mgame` 库）

| 集合 | 唯一索引 | 关键字段 |
|---|---|---|
| `users` | `account` | `uid` `ip` `version` `created_at` `updated_at`（**不存角色/在线角色**） |
| `roles` | `role_id`、`name`（全局唯一） | `role_id`(全局) `account` `name` `level` `created_at` |

`users` 不存角色列表：登录时按 `roles.account` 反查派生内存 `RoleIDs`；`OnlineRole` 仅进程内存。Redis 只存两类键：`mgame:user:<account>`（读缓存，登录/改角色后失效，值含 `role_ids`）与 `mgame:role:id`（全局自增）。

## 服务注册与发现

启用 `registry.endpoints` 后：

- game 监听就绪后注册 `/mgame/services/game/<id>`，gate 监听就绪后注册 `/mgame/services/gate/<id>`
- gate 通过 etcd watch 发现 game 实例并自动增删连接；未启用时使用静态 `gameServers`
- `SIGINT` / `SIGTERM` 优雅下线：注销自身键后再退出

```bash
etcdctl get --prefix /mgame/services/            # 查看注册结果
```

## 鉴权

1. **客户端 → gate**：`auth.mode=dev` 时 token 为占位；`jwt` 时本地 HS256 验签，`uid` / `account` 以 token 为准。未登录消息一律拒绝。
2. **gate → game**：`RegisterGate` 携带 `gate_id|ts|nonce` 的 HMAC-SHA256 签名（30s 时间窗），game 校验后才接受该会话的业务消息。
3. JWT 测试需在客户端传 `-jwt-secret`，与 `auth.jwtSecret` 一致。

## 开发约定

详见 [AGENTS.md](./AGENTS.md)。要点：

- 改 `.proto` 后运行 `./proto/gen.sh` 重新生成，勿手改 `*.pb.go` / `msgid.go`
- `gate.proto` 与 `inner.proto` 必须同属 proto package `gate`；msgid 只由 `inner.proto` 生成一份 `proto/msgid.go`
- 提交前：`gofmt -l .`、`go build ./...`、`go vet ./...`

## License

[MIT](./LICENSE)
