# AGENTS.md

Go 游戏服务器项目：gate/game 双服务，单模块；已 git 初始化（远端 `github.com/zhenhuasu/mgame`，分支 `main`），有 `README.md` 与 MIT `LICENSE`；无 CI、测试或 lint 配置。构建输出请用 `go build -o bin/ ./gate ./game`（`/gate`、`/game` 是源码目录，构建产物放 `bin/`，`.gitignore` 已忽略 `/bin/`）。

## 架构（已确认的意图）

- 单模块 `github.com/zhenhuasu/mgame`（根目录一个 `go.mod`），不拆多模块。
- `core/` — 目录容器，按功能分包（当前 `net`、`svcproc`、`auth`、`registry`，后续其他功能加兄弟包）
  - `core/auth/` — gate↔game 内网握手共享签名：`SignGate`/`VerifyGate`（HMAC-SHA256 over `gate_id|ts|nonce`，30s 时间窗，constant-time 比较）；gate/game 各自 import，勿重复实现
  - `core/net/` — cellnet 公共装配与统一消息分发（两服务共用；包名 `net`，import `github.com/zhenhuasu/mgame/core/net`）
    - `core.go` — `Init`（建 EventQueue + 按 `InitOptions.LogNames` 正则设日志级别 + 按 `InitOptions.Registry` 建注册中心，**返回 error**，不起 loop）、`SignalContext()`（SIGINT/SIGTERM ctx，优雅下线入口）、`Queue()`（暴露队列给 gate ws 复用）、`Registry()`/`CloseRegistry()`/`RegisterSelf(ctx, inst)`（托管注册中心：未启用时 `RegisterSelf` 返回可安全 Close 的 no-op）、`Post(fn)`（投递到事件队列执行）、`Bind(peer, procName)`（绑统一 dispatch）、`Listen(opt Options)`（tcp.Acceptor 对内监听）、`Connect(opt Options)`（tcp.Connector + 可选重连，返回 peer 供 Send）、`Run`（StartLoop+Wait）、`RunContext(ctx)`（同 Run，ctx 取消时 StopLoop，优雅下线入口）；`Options` 为 Listen/Connect 合并参数（PeerType 空时各自兜底 Acceptor/Connector，ProcName 空时兜底 `"svc.backend"`，Reconnect 仅 Connect 用）；blank imports（`peer/tcp`、`codec/gogopb`、`_ github.com/zhenhuasu/mgame/core/svcproc`、`_ github.com/zhenhuasu/mgame/proto`）集中在此；`registry` 日志名由 Init 自动纳入 LogLevel，调用方无需再传
    - `dispatch.go` — `Register(key, h)` 统一注册表（键：消息ID / 协议名 `"gate.X"` / 消息实例 / 系统事件名 `"SessionClosed"`；同键多 handler 依次执行）、`RegisterDefault(h)`（业务消息未命中兜底，如 gate 转发）、统一 `dispatch`（系统事件按类型名、业务消息按 meta.ID 查表）
  - `core/svcproc/` — gate↔game 后端自定义 proc（`setup.go` 注册 `"svc.backend"`、`transmitter.go` LTV 帧收发、`hooker.go` rpc/relay 解析+收发日志，logger 名 `svcproc` 被两服务 LogNames `"svc"` 覆盖）；骨架复制 cellnet `proc/tcp`（wire 兼容），`core/net` 两服务 Listen/Connect 默认 ProcName 即此名，后续改帧/统计填 `TODO` 处；`SetCallback` 必须 `proc.NewQueuedEventCallback` 保单队列串行；`RegisterProcessor` 重名 panic，仅 `core/net/core.go` 一处 blank import；**不得反向 import `core/net`**（防环）
  - `core/registry/` — 统一服务注册与发现（etcd，依赖 `go.etcd.io/etcd/client/v3`）：`registry.go` 定义 `Registry`（`Register`/`Discover`/`Watch`）+ `Instance` + `NewFromConfig`（endpoints 空返回 `(nil,nil)`）、`etcd.go` 租约注册（Grant+Put+KeepAlive，续约断开自动重注册、`Close` 删键）、`static.go` 进程内实现（测试/联调用）；**实例化由 `net.Init` 托管**（不再在 main 里单独 New/Close），logger 名 `registry` 也由 Init 自动纳入 LogLevel
- `gate/` — 网关服务（ws/wss 接入客户端；tcp Connector 连 game）
  - `gate/main.go` — 三步入口：`net.SignalContext()` 建 ctx → `net.Init`（队列+日志+注册中心，返回 error，`Registry: cfg.Registry`，`defer net.CloseRegistry()`）→ `network.ConfigureAuth(cfg)`（**必须调**，注入 token 校验器 + `gateSecret`，漏调则内网 HMAC 空签名被 game 拒）→ `network.Connect(ctx,cfg)` → `network.Listen(ctx,cfg)`；main 不直接接触 cellnet/registry
  - `gate/network/` — `server.go` 的 `Listen(ctx,cfg)`：ws 装配（gorillaws.Acceptor、wss 证书、`sessionLookup` 注入）保持 gate 独有，队列用 `net.Queue()`、分发用 `net.Bind(p,"gate.ws")`（自定义 proc，见下），`p.Start()` 后 `net.RegisterSelf` 注册自身（`Service=gate`，`ID=GateID`，`Addr=Advertise`，未启用注册中心时 no-op），结尾 `net.RunContext(ctx)` 阻塞；`games.go` 的 `Connect(ctx,cfg)`：`net.Registry()` 非 nil 用 `RegistryDiscovery`（发现 game），否则 `StaticDiscovery(cfg.GameServers)`，GamePool + 注册对内 handler（onConnected/onClosed/onRegisterAck/onForwardS2C），`start` 订阅发现回调 `reconcile` 增删 connector；`discovery.go` `Discovery{Addrs,Watch}` 接口 + `StaticDiscovery`/`RegistryDiscovery`；`handler.go` `registerWsHandlers`：注册 ws 侧 handler（onAccepted/onClosed/onPing/onLogin/onLogout/onBroadcast）+ `RegisterDefault(forwardC2S)`；`manager.go` 登录态注册表 + 广播 + `SessionByUID`/`UIDBySession`；`auth.go` `TokenVerifier`/`VerifyToken`（`ConfigureAuth` 按 `auth.mode` 注入 dev 占位或 JWT HS256 验签，uid/account 以 token 为准）+ 会话 TTL + `gateSecret`
  - `gate/network/wproc/` — gate ws 自定义 proc（`setup.go` 注册 `"gate.ws"`、`transmitter.go` 帧收发、`hooker.go` 收发拦截，logger 名 `gateproc` 被 LogNames `"gate"` 覆盖）；骨架复制 cellnet `proc/gorillaws`（wire 兼容，客户端仍绑 `"gorillaws.ltv"`），后续改帧/加密/统计填 `TODO` 处；`SetCallback` 必须 `proc.NewQueuedEventCallback` 保单队列串行；`RegisterProcessor` 重名 panic，该包仅 gate 一处 blank import
  - `client/` — 测试客户端（Ping / 登录-广播-退出-Hello，flags 驱动；原 `gate/client`，已上移到根）
- `game/` — 游戏服务（tcp Acceptor 接 gate；处理 ForwardC2S 业务、维护玩家在线表）
  - `game/main.go` — 两步入口（与 gate 对称）：`net.SignalContext()` → `net.Init`（队列+日志+注册中心，返回 error，`Registry: cfg.Registry`，`defer net.CloseRegistry()`）→ `network.Listen(ctx,cfg)`
  - `game/network/` — `server.go` 的 `Listen(ctx,cfg)`：`registerHandlers` + `net.Listen(net.Options{...})` + `net.RegisterSelf`（`Service=game`，`ID=GameID`，`Addr=Advertise`）+ `net.RunContext(ctx)`；`handler.go` 注册 SessionAccepted/Closed、RegisterGate（校验 HMAC，未认证会话的 PlayerOnline/Offline/ForwardC2S 一律丢弃）、PlayerOnline/Offline、ForwardC2S 并处理
- `proto/` — **全局** protobuf 包（`github.com/zhenhuasu/mgame/proto`），gate/game 共用；改 `.proto` 后跑 `./proto/gen.sh` 重新生成，勿手改 `*.pb.go` / `*.msgid.go`。`gate.proto`（客户端协议）与 `inner.proto`（gate↔game 内网协议）**必须同属一个 proto package `gate`**（protoc-gen-msg 按 proto package 名生成 Go package，同目录不能有两个 Go package）
- `config/` — **全局** 配置包，分服务：`gate.go`+`gate.yaml`（及 `gate.wss.yaml`）、`game.go`+`game.yaml`（多实例 `game2.yaml`）、`registry.go`（两服务共用的 `Registry{Endpoints,Prefix,TTL}`，`Enabled()`/`ApplyDefaults()`）

## 运行与验证

```bash
go build ./...
go vet ./...

# ws
go run ./gate -config config/gate.yaml
go run ./client -addr ws://127.0.0.1:8801/ws                          # Ping
go run ./client -account a -uid 1001 -token t -wait 1s -logout         # 登录/退出
go run ./client -account a -uid 1001 -token t -broadcast -content hi   # 全局广播
go run ./client -account a -uid 1001 -token t -broadcast -uids 1002    # 定向广播

# wss（证书在 certs/，已 gitignore；用 openssl 自签生成）
go run ./gate -config config/gate.wss.yaml

# game（gate 连入前需先起；多实例做负载均衡）
go run ./game -config config/game.yaml    # 127.0.0.1:8901
go run ./game -config config/game2.yaml   # 127.0.0.1:8902

# gate↔game 全链路（需 game 在线）
go run ./client -account a -uid 1001 -token t -hello -content hi   # Hello 经 gate→game→gate 回包
go run ./client -account a -uid 1001 -token t -hello -logout       # 退出触发 PlayerOffline

# 注册发现（把各 yaml 的 registry.endpoints 设为 ["127.0.0.1:2379"] 后）
etcd   # 或 docker run -d -p 2379:2379 quay.io/coreos/etcd:v3.5.0 etcd --advertise-client-urls http://127.0.0.1:2379 --listen-client-urls http://0.0.0.0:2379
etcdctl get --prefix /mgame/services/            # 看注册结果
go run ./game -config config/game.yaml             # 启动即注册 /mgame/services/game/<id>
go run ./gate -config config/gate.yaml             # 启动即发现 game 并注册 gate
kill -TERM <pid>                                    # 优雅下线：键立即消失
```

## gate↔game 连接层

- **方向**：gate = `tcp.Connector`（主动连 game，`SetReconnectDuration(1s)` 自动重连），game = `tcp.Acceptor`；地址来自 `registry` 发现 game 服务（`registry.endpoints` 非空时纯 etcd watch），否则回退 `config.Gate.GameServers` 静态列表。
- **握手**：连接成功后 gate 发 `RegisterGate{gate_id}`，game 回 `RegisterAck`。
- **路由**：登录时 `GamePool.Assign(uid)` RoundRobin 选实例，`uid→addr` 存 GamePool；重连后按路由补发 `PlayerOnline`（game 状态已丢）。
- **玩家生命周期** `PlayerOnline/PlayerOffline`：reason 0=主动退出 1=断线 2=顶号迁移到其他实例（旧实例下线）。
- **业务转发**：gate 未处理的消息 → `ForwardC2S{uid, msg_id, payload}`（信封不解析业务）→ game 按 msgID 解码处理 → `ForwardS2C` 回包 → gate 查 `SessionByUID` 发 `RawPacket` 给客户端。
- 所有 gate 事件（客户端 + game 连接）共用**同一个 EventQueue** 串行处理（`net.Init` 创建，ws 与对内连接都从 `net.Queue()` 取）。

## core 装配与注册（启动约定）

- **启动顺序**：`net.Init` → `Register`（藏在 `network.Connect`/`network.Listen` 开头）→ 建 peer → `net.Run`/`net.RunContext`。事件在 `Run` 前只入队不派发（`SessionQueuedCall` → `queue.Post`），所以先 Connect 后 Listen、handler 注册晚于 peer 启动也安全。
- **统一 dispatch**：Listen/Connect 内部与 gate ws 装配（`net.Bind`）共用同一个处理函数；系统事件（实现 `SystemMessage()`）按**类型名**（`"SessionClosed"`）查表，业务消息按 **meta.ID** 查表。
- **协议名注册**：`Register("gate.X", h)` 经 `util.StringHash` → `MessageMetaByID` 归一到消息 ID（cellnet 的 `FullName()` 是 Go 包名 `proto.X`，与 proto 包名 `gate.X` 不同，两条路都试）。
- **SessionClosed 分流**：gate 进程里 ws 客户端与 game 连接都会触发全局 key `"SessionClosed"`，两个 handler 都注册、各自按 `ev.Session().Peer().TypeName()`（`gorillaws.Acceptor` / `tcp.Connector`）过滤；game 进程只有一处，不过滤。
- **默认兜底**：`RegisterDefault` 只对业务消息生效（gate 用它做未注册消息的 C2S 转发），系统事件未注册不走兜底。

## 登录态与广播（gate）

- 状态在 `network.Manager`：`byUID`（uid→LoginInfo）+ `bySession`（sessionID→uid），断线 `SessionClosed` 清理（幂等）。
- **顶号**：同 uid 重复登录 → 旧会话收 `KickNtf` 后 Close，新会话接管；`RemoveBySession` 只删仍指向该会话的记录，不误删新登录态。
- **广播统一入口** `Manager.BroadcastToUids(uids, msg)`：uids 空=全局（仅已登录会话），非空=按列表——预留 game 服务消息驱动；先取快照再 Send，锁外做网络 IO。
- `BroadcastReq` 是**联调后门**（客户端可触发广播），仅 `auth.mode=dev` 注册；生产（jwt）不暴露。

## 鉴权

- **客户端**：登录时 `VerifyToken` 校验 token，uid/account 以校验结果为准（不信任客户端自报 uid）。`auth.mode=dev` 保留占位校验（account/uid/token 非空），`jwt` 用 HS256 本地验签并校验 `exp`（依赖 `github.com/golang-jwt/jwt/v5`）。`auth.tokenTTL>0` 时会话过期，业务消息经 `requireLogin` 刷新 `Touch`，过期踢下线。
- **登录守卫**：`requireLogin(ev)` 用于 `forwardC2S`/`onBroadcast`，未登录或过期直接丢弃。
- **内网**：gate 连上 game 发 `RegisterGate{gate_id,ts,nonce,hmac}`，`hmac=base64(HMAC-SHA256(gateSecret, gate_id|ts|nonce))`；game 校验时间窗（±30s）+ constant-time 比较，失败回 `code=1` 并 Close。`gateSecret` 为空=不校验（仅联调），两侧须配相同值。

## 服务注册与发现（etcd）

- **键布局**：`{prefix}/services/{service}/{id}` → `{"addr":"...","meta":{...}}`；服务名常量 `registry.ServiceGate`/`ServiceGame`，后续服务自行传名。
- **配置**：各服务 `registry.endpoints` 非空即启用（默认 `prefix=/mgame`、`ttl=10s`）；`endpoints` 空 = 不启用——gate 回退静态 `gameServers`，game 不注册。
- **注册时机**：都在**监听启动之后**注册（game 在 `net.Listen` 后、gate 在 `p.Start()` 后），确保别人拿到地址即可拨号；`GameID`/`GateID` 为空自动生成 `<service>-<pid>`，`advertise` 为空回退 `listen`（多机部署必须配，`0.0.0.0` 注册出来拨不通）。
- **租约**：`Grant+Put(WithLease)+KeepAlive` 后台续约；续约通道关闭（etcd 不可达/租约过期）自动重新 Grant+Put；进程崩溃由 ttl 自动清理。
- **优雅下线**：main 用 `net.SignalContext()`（SIGINT/SIGTERM）→ ctx 取消 → `net.RunContext` 停事件循环 → `defer` 执行 `Registration.Close()`（删键，`Listen` 内）与 `net.CloseRegistry()`。**`net.Run` 无此语义，两服务已改用 `RunContext`。**
- **gate 侧发现**：`RegistryDiscovery` 包装 `reg.Watch("game")`（latest-wins 快照），watch 启动失败（etcd 不可达）回退静态地址；`GamePool.reconcile` 只做**增删 connector**，不做玩家迁移——game 消失后其路由玩家需重连重登。
- **已知坑**：gate 的 `network.ConfigureAuth(cfg)` 必须在 `Connect` 前调用（漏调 → `gateSecret` 为空 → `RegisterGate` 无签名 → game 全部拒连并 1s 重连风暴）。

## cellnet 诡异点（踩过的坑）

- 框架：`github.com/davyxu/cellnet@v4.1.0`（+incompatible，无 go.mod）。
- 必须 blank import 才能注册 peer/proc/codec：`peer/tcp`、`codec/gogopb`、`_ github.com/zhenhuasu/mgame/core/svcproc`（注册 `"svc.backend"`，替代 `proc/tcp`）、`_ github.com/zhenhuasu/mgame/proto` 已集中在 `core/net/core.go`；gate 另需 `peer/gorillaws` + `_ github.com/zhenhuasu/mgame/gate/network/wproc`（注册 `"gate.ws"`，替代 `proc/gorillaws`，在 `gate/network/server.go`）。tcp peer 依赖 `github.com/davyxu/protoplus`（已在 go.mod）。
- **WS/TCP 帧格式**：2 字节小端 msgID + protobuf body；msgID 是 **uint16**（`util.StringHash("proto包名.消息名")`），注册时 ID=0 或重复会 panic。
- cellnet 没有 `cellnet.Connector` 接口，tcp connector 断言用 `cellnet.TCPConnector`（含 `Session()`/`SetReconnectDuration`）；peer 名取 `ev.Session().Peer().(cellnet.PeerProperty).Name()`（Peer 接口本身无 Name）。
- tcp connector 的 `SessionConnected` 每次重连都会触发——`RegisterGate`/补发 `PlayerOnline` 挂在该事件上即可。
- wss 由 `WSAcceptor.SetHttps(certFile, keyFile)` 切换（`config.protocol: wss` 时调用）；地址需 `scheme://host:port/path` 格式（`config.Gate.Addr()` 已拼好）。
- cellnet 自带 ws connector 默认不信任自签证书；wss 客户端测试需自行设置 `TLSClientConfig.InsecureSkipVerify` 或用受信证书。
- `protoc-gen-msg` 的 `--msg_out` 参数必须是**输出文件名**（如 `gate.msgid.go:.`），传目录会报 "First file chunk..." 错误。
- cellnet 日志走 `davyxu/golog`，按名注册、默认 debug；`golog.SetLevelByString` 的 name 是**正则子串**，`"gorillaws"` 同时命中 `gorillaws`+`gorillawsproc`，`"gate"` 同时命中 `gate`+`gateproc`，`"tcp"` 命中 `tcppeer`（gate/game 已不注册 `tcpproc`，proc 由 `svcproc`/`wproc` 替代）。配置项 `logLevel`（debug/info/warn/error）在 `net.Init` 开头统一设置（经 `InitOptions.LogNames`），设 `info` 即可关掉收发消息 dump。

## protoc 工具链

```bash
brew install protobuf
go install github.com/gogo/protobuf/protoc-gen-gogofaster@v1.3.2
go install github.com/davyxu/cellnet/protoc-gen-msg@v4.1.0
./proto/gen.sh
```

## 约定与怪癖

- 禁用 `github.com/pkg/errors` 和 `golang.org/x/net/context`（见 `.idea/go.imports.xml` 的排除列表）。
- `.idea/` 中 `workspace.xml` 等已被 `.idea/.gitignore` 忽略，勿提交无关 IDE 文件；`certs/`、构建产物见根 `.gitignore`。
- 目录尚未 git 初始化，`git` 命令会失败，需要时先 `git init`。
- 尚无 lint / typecheck / test 命令；新增工具链后必须同步更新本文件。
- 文档与代码冲突时，以代码为准。
