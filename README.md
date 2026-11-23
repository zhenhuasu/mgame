# mgame

Go 分布式游戏服务器骨架：**gate（网关）+ game（游戏服）双服务**，支持 etcd 服务注册发现、JWT 客户端鉴权、gate↔game HMAC 内网握手。

- 模块路径：`github.com/zhenhuasu/mgame`
- 单 Go 模块，无外部数据库依赖，`go build ./...` 即可运行

## 目录结构

```
mgame/
├── core/                # 跨服务公共能力
│   ├── net/             # cellnet 装配、事件队列、统一消息分发、注册中心托管
│   ├── svcproc/         # gate↔game 后端自定义 proc（LTV 帧）
│   ├── auth/            # gate↔game HMAC-SHA256 签名/校验
│   └── registry/        # 服务注册与发现（etcd / 内存静态实现）
├── gate/                # 网关服务：ws/wss 接入客户端，tcp 连 game
│   ├── main.go
│   └── network/         # ws 装配、game 连接池与发现、登录态、鉴权
├── game/                # 游戏服务：处理业务与玩家在线表
├── client/              # 测试客户端（Ping / 登录 / 广播 / Hello）
├── proto/               # protobuf（gate.proto 客户端协议 + inner.proto 内网协议）
├── config/              # 配置加载与各服务 yaml
└── AGENTS.md            # 项目约定与实现细节
```

## 快速开始

需要 Go 1.26+。

```bash
go build ./...
go vet ./...

# 1. 起游戏服（默认 127.0.0.1:8901）
go run ./game -config config/game.yaml
# 第二个实例（负载均衡用，127.0.0.1:8902）
go run ./game -config config/game2.yaml

# 2. 起网关（ws://0.0.0.0:8801/ws）
go run ./gate -config config/gate.yaml

# 3. 测试客户端
go run ./client -addr ws://127.0.0.1:8801/ws                          # Ping
go run ./client -account a -uid 1001 -token t -wait 1s -logout         # 登录/退出
go run ./client -account a -uid 1001 -token t -broadcast -content hi   # 全局广播
go run ./client -account a -uid 1001 -token t -hello -content hi       # Hello 经 gate→game→gate 回包
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

wss 示例见 `config/gate.wss.yaml`，证书用 `certs/`（已被 gitignore，需自行用 openssl 生成）。

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

- 改 `.proto` 后运行 `./proto/gen.sh` 重新生成，勿手改 `*.pb.go` / `*.msgid.go`
- `gate.proto` 与 `inner.proto` 必须同属 proto package `gate`
- 提交前：`gofmt -l .`、`go build ./...`、`go vet ./...`

## License

[MIT](./LICENSE)
