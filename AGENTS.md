# AGENTS.md

Go 游戏服务器项目：gate/game/guild 三服务，单模块；已 git 初始化（远端 `github.com/zhenhuasu/mgame`，分支 `main`），有 `README.md` 与 MIT `LICENSE`；无 CI、测试或 lint 配置。构建输出请用 `go build -o bin/ ./gate ./game ./guild`（`/gate`、`/game`、`/guild` 是源码目录，构建产物放 `bin/`，`.gitignore` 已忽略 `/bin/`）。

## 架构（已确认的意图）

- 单模块 `github.com/zhenhuasu/mgame`（根目录一个 `go.mod`），不拆多模块。
- `core/` — 目录容器，按功能分包（当前 `net`、`svcproc`、`auth`、`registry`、`redis`、`mongo`，后续其他功能加兄弟包）
  - `core/auth/` — 内网握手共享签名：通用 `SignPeer`/`VerifyPeer`（HMAC-SHA256 over `id|ts|nonce`，30s 时间窗，constant-time 比较）+ `SignGate`/`VerifyGate` 包装；gate↔game 与 game↔guild 共用，勿重复实现
  - `core/net/` — cellnet 公共装配与统一消息分发（gate/game/guild 共用；包名 `net`，import `github.com/zhenhuasu/mgame/core/net`）
    - `init.go` — `Init`（建 EventQueue + 按 `InitOptions.LogNames` 正则设日志级别 + 按 `InitOptions.Registry` 建注册中心，**返回 error**，不起 loop）、`SignalContext()`（SIGINT/SIGTERM ctx，优雅下线入口）、`Queue()`（暴露队列给 gate ws 复用）、`Registry()`/`CloseRegistry()`/`RegisterSelf(ctx, inst)`（托管注册中心：未启用时 `RegisterSelf` 返回可安全 Close 的 no-op）、`Post(fn)`（投递到事件队列执行）、`Bind(peer, procName)`（绑统一 dispatch）、`Listen(opt Options)`（tcp.Acceptor 对内监听）、`Connect(opt Options)`（tcp.Connector + 可选重连，返回 peer 供 Send）、`Run`（StartLoop+Wait）、`RunContext(ctx)`（同 Run，ctx 取消时 StopLoop，优雅下线入口）；`Options` 为 Listen/Connect 合并参数（PeerType 空时各自兜底 Acceptor/Connector，ProcName 空时兜底 `"svc.backend"`，Reconnect 仅 Connect 用）；blank imports（`peer/tcp`、`codec/gogopb`、`_ github.com/zhenhuasu/mgame/core/svcproc`、`_ github.com/zhenhuasu/mgame/proto`）集中在此；`registry` 日志名由 Init 自动纳入 LogLevel，调用方无需再传（`discover` 需在 `LogNames` 显式列出）
    - `dispatch.go` — `Register(key, h)` 统一注册表（键：消息ID / 协议名 `"gate.X"` / 消息实例 / 系统事件名 `"SessionClosed"`；同键多 handler 依次执行）、`RegisterDefault(h)`（业务消息未命中兜底，如 gate 转发）、统一 `dispatch`（系统事件按类型名、业务消息按 meta.ID 查表）
  - `core/svcproc/` — **所有后端通信共用**的自定义 proc（gate→game、game→guild；`setup.go` 注册 `"svc.backend"`、`transmitter.go` LTV 帧收发、`event.go` Transmit 解包、`hooker.go` 仅入站解包（出站原样透传，不引入 rpc/relay/msglog），logger 名 `svcproc` 被各服务 LogNames `"svc"` 覆盖）；`hooker.OnInboundEvent` 解 `*pb.Transmit`（`codec.DecodeMessage`）为最终协议并返回 `*TransmitEvent`，携带 `TransmitContext{UID,RoleID,RoleName,RoleLevel}`（业务层 `svcproc.ContextOf(ev)` 取），解码失败日志丢弃；**业务层只注册最终协议**；骨架复制 cellnet `proc/tcp`（wire 兼容），`core/net` Listen/Connect 默认 ProcName 即此名，后续改帧/统计填 `TODO` 处；`SetCallback` 必须 `proc.NewQueuedEventCallback` 保单队列串行；`RegisterProcessor` 重名 panic，仅 `core/net/init.go` 一处 blank import；**不得反向 import `core/net`**（防环）
  - `core/registry/` — 统一服务注册与发现（etcd，依赖 `go.etcd.io/etcd/client/v3`）：`registry.go` 定义 `Registry`（`Register`/`Discover`/`Watch`）+ `Instance` + `NewFromConfig`（endpoints 空返回 `(nil,nil)`）、`etcd.go` 租约注册（Grant+Put+KeepAlive，续约断开自动重注册、`Close` 删键）、`static.go` 进程内实现（测试/联调用）；**实例化由 `net.Init` 托管**（不再在 main 里单独 New/Close），logger 名 `registry` 也由 Init 自动纳入 LogLevel
  - `core/redis/` — **各服务共用的 Redis 客户端**（依赖 `github.com/redis/go-redis/v9`，以 `goredis` 别名导入避免包名冲突）：`redis.go` `Client` + **`New(config.Redis)`（启动 `Ping` 3s 校验，失败返回 error → 调用方 fail-fast）** + `Get/Set/Del/Incr/Raw/Close`；**不碰业务语义**——键前缀/过期/缓存策略由各业务自带（game 见 `config.UserData` 与 `repo.NewCached`）；`Incr` 供全局 ID 生成（单命令原子，**无需分布式锁**）；无 logger（失败直接返回错误）
  - `core/mongo/` — **各服务共用的 MongoDB 连接与通用助手**（依赖 `go.mongodb.org/mongo-driver/v2`）：`mongo.go` **`Open(ctx, config.Mongo) (*Client, error)`**（Enabled 校验 → v2 `mongo.Connect(opts)`（不收 ctx）→ 带超时 ctx `Ping` 3s → 选定数据库；失败返回 error → 调用方 fail-fast）+ `Client{Database,Collection,Raw,Close}`；`crud.go` **泛型反复样板**：`Get[T]`（`ErrNoDocuments`→`(zero,false,nil)`）、`Find[T]`、`Insert[T]`、`UpdateOne/UpdateMany`、`Upsert`、`DeleteOne/DeleteMany`、`Count`、`Aggregate[T]`，选项用 v2 `options.Lister[...]`，错误统一带集合名；`index.go`：`Ensure`、`DropIfExists`（吞 IndexNotFound code 27）、`IsDuplicate`、`DuplicateKeyValue`。**文档结构/集合名/索引定义/业务错误仍归各业务仓储（game 见 `game/repo`，guild 见 `guild/repo`）**
  - `core/discover/` — 通用实例发现：`Discovery{Addrs,Watch}` + `Static`（静态列表，立即回调）+ `NewRegistry(reg, service, fallback)`（etcd watch latest-wins 快照，watch 启动失败回退静态）；gate 发现 game、game 发现 guild 共用（原 `gate/backend/discovery.go` 已迁入）；logger 名 `discover`（各服务 `LogNames` 已含 `"discover"`）
- `gate/` — 网关服务（ws/wss 接入客户端；tcp Connector 连 game）
  - `gate/main.go` — 入口：`flag` → `config.LoadGate` → `net.SignalContext()` 建 ctx → `net.Init`（队列+日志+注册中心，返回 error，`Registry: cfg.Registry`，`defer net.CloseRegistry()`）→ `run(ctx,cfg)`（见 app.go）；main 不直接接触 cellnet/registry
  - `gate/app.go` — package main 装配（**无中间 network 层**）：`frontend.Configure(cfg.Auth)`（**必须调**，注入 token 校验器）+ `backend.SetSecret(cfg.GateSecret)`（**必须调**，漏调则内网 HMAC 空签名被 game 拒）→ `session.New(tokenTTL)` → `net.Registry()` 非 nil 用 `discover.NewRegistry(reg, registry.ServiceGame, discover.Static(cfg.GameServers))` 否则静态地址 → `backend.New(...).Start(ctx)`（注册对内 handler + watch 发现）→ `frontend.New(cfg,mgr,pool).Start()`（注册客户端 handler + 起 ws）→ `net.RegisterSelf` 自注册（`Service=gate`，`ID=GateID`，`Addr=Advertise`，未启用时 no-op）→ `net.RunContext(ctx)` 阻塞，ctx 取消后 defer 注销注册
  - `gate/frontend/` — **与客户端通信**（ws）：`server.go` 的 `Start()`：ws 装配（gorillaws.Acceptor、wss 证书、`session.SetLookup` 注入会话查找）、队列 `net.Queue()`、`net.Bind(p,"gate.ws")`；`handler.go` 定义 `GameRouter` 接口（Assign/Unassign/RouteOf/SendTo，由 `backend.Pool` 实现，**frontend 不 import backend**）+ `Server{cfg,mgr,games}` + 客户端 handler（onAccepted/onClosed/onPing/onLogin/onLogout/onBroadcast/requireLogin/forwardC2S）+ `EncodeTransmit`；`handleLogin` 校验通过后 `mgr.Login`（顶号）→ `Assign` → `SendTo(PlayerLogin{uid,account,ip,version})`，**不即时回成功 LoginAck**（由 game 结果驱动，见 backend `onPlayerLoginAck`），失败路径（校验不过 / 无 game / 发送失败）回 `LoginAck{code:1}` 并回滚登录态与路由；`clientIP` 从 `ses.Raw()` 取客户端出口 IP；`auth.go` `TokenVerifier`/`VerifyToken`（`Configure` 按 `auth.mode` 注入 dev 占位或 JWT HS256 验签，uid/account 以 token 为准）+ `debugHandlers`（联调后门仅 dev 注册）
  - `gate/backend/` — **与 game 服务通信**：`pool.go` `Pool`（实现 `frontend.GameRouter`：连接/重连、RoundRobin 分配、uid 路由、`SendTo`、断线补发 PlayerOnline；`up` 记录各地址**是否已连通**，`pickLocked`/`SendTo` 只认 `up=true`——cellnet connector 的 `Session()` 建连前也非 nil，不能当连通判据）、`handler.go` onConnected/onClosed/onRegisterAck/**onPlayerLoginAck**（登录结果 → `hub.SessionByUID` 回投 `LoginAck{code,reason,uid,roles}`，失败码时回滚 `Unassign`+`RemoveBySession`）/onForwardS2C（回投经 `hub.SessionByUID`）、实例发现来自 `core/discover`（`Static`/`NewRegistry`）、`auth.go` `gateSecret` + `SetSecret` + `signRegisterGate`（HMAC 握手）；**backend 不 import frontend**
  - `gate/session/` — 共享登录态（frontend 维护、backend 回投查询，靠它避免 frontend↔backend 循环 import）：`manager.go` `Manager`/`LoginInfo`/`New(ttl)`（ttl 来自 `auth.tokenTTL`）+ `SetLookup`（frontend ws acceptor 注入）+ `SessionByUID`/`UIDBySession`/`Broadcast`/`BroadcastToUids`
  - `gate/frontend/wproc/` — gate ws 自定义 proc（`setup.go` 注册 `"gate.ws"`、`transmitter.go` 帧收发、`hooker.go` 收发拦截，logger 名 `gateproc` 被 LogNames `"gate"` 覆盖）；骨架复制 cellnet `proc/gorillaws`（wire 兼容，客户端仍绑 `"gorillaws.ltv"`），后续改帧/加密/统计填 `TODO` 处；`SetCallback` 必须 `proc.NewQueuedEventCallback` 保单队列串行；`RegisterProcessor` 重名 panic，该包仅 gate 一处 blank import
  - `client/` — 测试客户端（Ping / 登录 / 广播 / Hello / 进入游戏 / 公会，flags 驱动；`-guild create|join|leave|kick|transfer|notice|disband|info|list|invite|invites|respond|set-join-mode|set-position` + `-guild-name/-guild-id/-target-role/-target-name/-notice/-join-mode/-position/-accept`；`-version` 随登录上报，登录后打印 `LoginAck.roles`，再按 `-flow create|enter`（空=有角色进入、无角色先建）发 `CreateRoleReq`/`EnterGameReq` 并打印 Ack；原 `gate/client`，已上移到根）
- `game/` — 游戏服务（tcp Acceptor 接 gate；以 `account` 为唯一键加载用户、维护在线表、角色与进入游戏）
  - `game/main.go` — 入口：`net.SignalContext()` → `net.Init`（队列+日志+注册中心）→ **`mongo.Open`（fail-fast）→ `redis.New`（fail-fast）→ `repo.NewMongo` → `EnsureIndexes` → `repo.NewCached`（Mongo 持久 + Redis 读缓存）→ `service.NewUserManager(...)` → `guildclient.New(cfg.GameID, 发现)` + `SetForwarder` + `Start(ctx)` → `handler.New`** → `network.Listen(ctx,cfg,h)`；退出时 `Close`
  - `game/model/` — 领域结构：`User{Account,UID,IP,Version,RoleIDs,OnlineRole}` + `Role{ID,Account,Name,Level}`（JSON 标签）；**`RoleIDs` 由 `roles.account` 反查派生、不落库，`OnlineRole` 仅进程内存、不落库**（重启后需重新 EnterGame）；**登录不建角色**，`DefaultRoles` 已移除
  - `game/repo/` — **仓储：Mongo 为唯一持久层，Redis 只做读缓存**：`repo.go` 接口 `UserRepo{Login,Load,LoadRoles,LoadRole,CreateRole,EnsureIndexes}` + `ErrNotOnline`/`ErrRoleNameExists`；`mongo.go` 基于 `core/mongo` 泛型助手（`Get/Find/Insert/Count/Upsert`、`Ensure/DropIfExists/IsDuplicate`），两集合 `users`（唯一 `account`，字段 `uid/ip/version/created_at/updated_at`，**不存角色/online_role_id**）与 `roles`（唯一 `role_id`、`name`——**角色名全局唯一**，`role_id` 全局，含 `account` 归属），`Login` 为幂等 upsert（刷新登录信息）后回读；`Load` 读 user 文档并按 `account` 查 `roles` 投影 `{role_id}` 派生 `RoleIDs`；`LoadRoles(account)` 角色基本信息（登录回包）、`LoadRole(account,roleID)` 单角色完整数据（按 `account` 限定归属，EnterGame 用）、`LoadRoleByName(name)` 按全局唯一角色名查（公会邀请解析）；`CreateRole` **仅插 roles 不写 users**，重复键映射 `ErrRoleNameExists`（按 `name` 全局计数区分重名与撞 ID）；`EnsureIndexes` 先 Drop 旧的 `roles_account_name_uq` 再建 `roles_name_uq`（**幂等**）；`cache.go` **`NewCached(inner, kv, prefix, ttl)` 装饰器**：`Load` 读穿透（miss 回源并回填，值含 `role_ids`）、`Login/CreateRole` 写后 `Del` 失效、`LoadRoles/LoadRole` 直通不缓存，缓存错误仅 `Warnf` 不阻断业务；logger 名 `game.repo`
  - `game/service/` — 业务逻辑：`usermanager.go` **`UserManager{repo,idgen,users,autoCreateRole}`**（进程内在线表 `uid→OnlineUser{UID,Account,User,Session}`，**事件单队列串行，无锁**）+ `Login`（仓储 upsert + 有角色时 `LoadRoles` 取基本信息 + 登记在线）、`MarkOnline`（重连补登记，内存缺失回源 `Load`，`OnlineRole` 为 nil）、`Offline`（**仅清内存/摘表，不写库**）、`RemoveBySession`（gate 断开**只摘表不清在线角色**）、`CreateRole`（取全局 ID → 插仓储 → 内存 `RoleIDs` append）、`EnterGame`（`role_id=0` 仅当开关开且无角色时建默认角色；否则 **`LoadRole` 按 account 加载完整角色** → 仅内存置 `OnlineRole`，**不写库**）、`OnlineRole(uid)`/`RoleByName(name)`/`FindOnlineByRole(roleID)`（公会转发上下文与推送命中）；`roleid.go` **`RoleIDGen`**：`Next = kv.Incr("mgame:role:id",1)`（**原子，无需分布式锁**，失败留空洞可接受）；logger 名 `game.service`
  - `game/handler/` — 消息处理：`handler.go` `Handler{mgr,secret,guild,authed}` + `Register()`（须在起事件循环前调用）+ `registerAuthed`（队列内校验 gate 握手与 `svcproc.ContextOf`，业务函数收 `TransmitContext`）；`gate.go` SessionAccepted/Closed（**`mgr.RemoveBySession`**，按 peer 类型只处理 `tcp.Acceptor`）+ RegisterGate（校验 HMAC；Player* 未认证一律丢弃，客户端业务在队列内 wrapper 校验）；`player.go` **`PlayerLogin`（`mgr.Login` → `PlayerLoginAck{code,reason,roles}`，roles 为登录时加载的基本信息）**、PlayerOnline（`mgr.MarkOnline`）、PlayerOffline（`mgr.Offline` 仅清内存）；`forward.go` **直接注册最终协议 Hello / `CreateRoleReq` / `EnterGameReq`**（svcproc 已解包 Transmit），`guild.go` 逐个注册 13 个公会协议并绑定各自失败 Ack（`registerGuildReq`），`InviteMemberReq` 单独强类型处理（按名解析 role_id），统一经 `forwardGuild`/`sendGuild` 校验 `OnlineRole`、组 `Transmit{uid,role_id,role_name,role_level,msg_id,payload}` 交 `GuildSender.Send`，无可用 guild 本地回 `guild unavailable`；无协议 `switch`）→ `sendForwardS2C` 经 `ForwardS2C` 回包；`guild.go` 同时实现 `guildclient.Forwarder`（GuildS2C→客户端、GuildNotify→`FindOnlineByRole` 命中）；logger 名 `game.handler`
  - `game/network/` — `server.go` 仅传输装配：`Listen(ctx,cfg,h)` → `h.Register()` → `net.Listen` → `net.RegisterSelf(ServiceGame)` → `net.RunContext(ctx)`；**main 不接触 cellnet**
  - `game/guildclient/` — game→guild 内网：`pool.go` 每地址一个 `tcp.Connector`（1s 重连）轮询发送 `Transmit`（`Send(t *pb.Transmit)` 无就绪连接返回 false）、`handler.go` 处理 `RegisterAck`/`GuildS2C`/`GuildNotify` 并回调 `Forwarder`、`auth.go` `SetSecret`+`signRegisterGame`（`auth.SignPeer`）
- `guild/` — 公会服务（**独立进程**：Mongo 为公会数据唯一持久层，Redis 做全局 guildId 自增；game 连入，gate 零改动）
  - `guild/main.go` — `net.SignalContext` → `net.Init`（`LogNames` 含 guild/discover）→ `mongo.Open`/`redis.New`（fail-fast）→ `repo.NewMongo` + `EnsureIndexes` → `service.NewGuildManager` → `handler.New`+`SetNotifier` → `network.Listen`
  - `guild/model/` — `Guild{ID,Name,LeaderRoleID,Notice,Level,JoinMode,MemberCount}`、`GuildMember{GuildID,RoleID,RoleName,RoleLevel,Position}`、`GuildInvite`；职位 `1=会长,2=副会长,3=成员`，加入方式 `0=open,1=invite_only`，事件 `1=kicked,2=disbanded,3=transfer,4=position`
  - `guild/repo/` — `GuildRepo` 接口 + `Err*`；`mongo.go` 三集合 `guilds`（`guild_id`/`name` 唯一）、`guild_members`（`role_id` 唯一 → 一角色一公会；`(guild_id,role_id)` 唯一）、`guild_invites`（`(guild_id,role_id)` 唯一）；`AddMember` 先插成员再 `member_count<max` 条件 `$inc`，未命中回滚并回 `guild is full`（无事务下的容量原子控制）
  - `guild/service/` — `GuildManager` 权限/容量/邀请逻辑；`guildid.go` `GuildIDGen` 用 `kv.Incr("mgame:guild:id",1)`；推送经 `Notifier`（handler 实现）
  - `guild/handler/` — `handler.go` `RegisterGame`（`auth.VerifyPeer`，失败回 `RegisterAck{code:1}` 并 Close）、`reply`/`notify`；`requests.go` 逐个注册 14 个公会最终协议到强类型函数（`registerReq[T]`，`handle` 内校验握手与 `TransmitContext`），`handlers.go` 为每个协议独立业务函数（无 `switch`）→ `GuildManager`、回包 `GuildS2C{uid,msg_id,payload}`；`GuildNotify` 广播给全部已握手 game；事件单队列串行
  - `guild/network/` — `Listen`：注册 handler → `net.Listen` → `net.RegisterSelf(ServiceGuild)` → `net.RunContext`
- `proto/` — **全局** protobuf 包（`github.com/zhenhuasu/mgame/proto`），gate/game/guild 共用；改 `.proto` 后跑 `./proto/gen.sh` 重新生成，勿手改 `*.pb.go` / `msgid.go`。`gate.proto`（客户端协议：LoginReq.version、LoginAck.roles、RoleInfo、CreateRoleReq/Ack、EnterGameReq/Ack，公会 `GuildInfo/GuildMemberInfo/GuildInviteInfo` + 14 组 Req/Ack + `GuildInviteNtf/GuildEventNtf` + 三个枚举）与 `inner.proto`（内网协议：通用上行信封 `Transmit`（uid/role_id/role_name/role_level/msg_id/payload）、PlayerLogin/PlayerLoginAck、`ForwardS2C`、`RegisterGame`/`GuildS2C`/`GuildNotify`）**必须同属一个 proto package `gate`**（protoc-gen-msg 按 proto package 名生成 Go package，同目录不能有两个 Go package）；`inner.proto` `import "gate.proto"` 复用 `RoleInfo`，故 **msgid 只由 `inner.proto` 生成一份 `msgid.go`**（protoc-gen-msg 会把被 import 的文件也注册，按文件各生成一份会重复注册而 panic）
- `config/` — **全局** 配置包，分服务：`gate.go`+`gate.yaml`（及 `gate.wss.yaml`）、`game.go`+`game.yaml`（多实例 `game2.yaml`，含 `UserData{Prefix,TTL,AutoCreateRole}` `yaml:"user"`、`GuildServers []string`、`GuildSecret`，**`LoadGame` 校验 `mongo.uri` 与 `redis.addr` 非空，缺则启动失败**）、`guild.go`+`guild.yaml`（`GuildData{MaxMembers,NameMaxLen,NoticeMaxLen}` 默认 50/24/200，**`LoadGuild` 校验 mongo/redis，缺则启动失败**）、`mongo.go`（**各服务共用** `Mongo{URI,Database}`，`Enabled()`/`ApplyDefaults()` db=`mgame`）、`registry.go`（各服务共用的 `Registry{Endpoints,Prefix,TTL}`，`Enabled()`/`ApplyDefaults()`）、`redis.go`（**各服务共用的连接** `Redis{Addr,Password,DB}`，`Enabled()`=Addr 非空，无 ApplyDefaults——键前缀/过期归各业务配置）

## 运行与验证

```bash
go build ./...
go vet ./...

# guild（可选；game 启动时连它；公会数据在 Mongo，需 Redis 做 guildId 自增）
go run ./guild -config config/guild.yaml   # 127.0.0.1:8903

# ws
go run ./gate -config config/gate.yaml
go run ./client -addr ws://127.0.0.1:8801/ws                          # Ping
go run ./client -account a -uid 1001 -token t -wait 1s -logout         # 登录/退出
go run ./client -account a -uid 1001 -token t -broadcast -content hi   # 全局广播
go run ./client -account a -uid 1001 -token t -broadcast -uids 1002    # 定向广播

# wss（证书在 certs/，已 gitignore；用 openssl 自签生成）
go run ./gate -config config/gate.wss.yaml

# game（gate 连入前需先起；多实例做负载均衡；需本机 MongoDB 27017 + Redis 6379，任一连不上启动失败）
go run ./game -config config/game.yaml    # 127.0.0.1:8901
go run ./game -config config/game2.yaml   # 127.0.0.1:8902

# gate↔game 全链路（需 game 在线）
go run ./client -account a -uid 1001 -token t -hello -content hi   # Hello 经 gate→game→gate 回包
go run ./client -account a -uid 1001 -token t -hello -logout       # 退出触发 PlayerOffline

# 登录与进入游戏（新账号登录 roles 为空，登录不建角色）
go run ./client -account a -uid 1001 -token t -version v1.0.0            # 自动建角色并进入（默认 flow）
go run ./client -account a -uid 1001 -token t -flow enter -role 3        # 直接进入指定角色
go run ./client -account a -uid 1001 -token t -flow create -role-name b  # 强制新建角色并进入
go run ./client -account a -uid 1001 -token t -flow enter                # role_id=0（开关关则报 role_id required）

# 公会（需 guild 在线；角色须先 EnterGame）
go run ./client -account a -uid 1001 -token t -flow create -guild create -guild-name G1  # 建会（会长）
go run ./client -account b -uid 1002 -token t -flow create -guild join -guild-id 1       # 直接加入（open）
go run ./client -account a -uid 1001 -token t -guild info                                # 自己公会+成员
go run ./client -account a -uid 1001 -token t -guild list                                # 公会列表
go run ./client -account a -uid 1001 -token t -guild invite -target-name b               # 按角色名邀请
go run ./client -account b -uid 1002 -token t -guild invites                             # 我的邀请
go run ./client -account b -uid 1002 -token t -guild respond -guild-id 1 -accept         # 接受邀请
go run ./client -account a -uid 1001 -token t -guild set-position -target-role 29 -position vice
go run ./client -account a -uid 1001 -token t -guild notice -notice hi
go run ./client -account a -uid 1001 -token t -guild set-join-mode -join-mode invite_only
go run ./client -account a -uid 1001 -token t -guild transfer -target-role 29
go run ./client -account b -uid 1002 -token t -guild kick -target-role 28
go run ./client -account b -uid 1002 -token t -guild disband
mongosh mgame --quiet --eval 'db.guilds.find().toArray()'                 # 公会
mongosh mgame --quiet --eval 'db.guild_members.find().toArray()'          # 成员
mongosh mgame --quiet --eval 'db.guild_invites.find().toArray()'          # 邀请
redis-cli get mgame:guild:id                                              # 全局公会 ID 计数器
mongosh mgame --quiet --eval 'db.users.find().toArray()'                 # users 集合（不存角色/online_role_id）
mongosh mgame --quiet --eval 'db.roles.find().toArray()'                 # roles 集合（role_id 全局、account 归属、name 全局唯一）
redis-cli get mgame:user:a    # 读缓存（登录/改角色后失效，miss 回源回填）
redis-cli get mgame:role:id   # 全局角色 ID 计数器（INCR）

# 注册发现（把各 yaml 的 registry.endpoints 设为 ["127.0.0.1:2379"] 后）
etcd   # 或 docker run -d -p 2379:2379 quay.io/coreos/etcd:v3.5.0 etcd --advertise-client-urls http://127.0.0.1:2379 --listen-client-urls http://0.0.0.0:2379
etcdctl get --prefix /mgame/services/            # 看注册结果
go run ./game -config config/game.yaml             # 启动即注册 /mgame/services/game/<id>
go run ./gate -config config/gate.yaml             # 启动即发现 game 并注册 gate
kill -TERM <pid>                                    # 优雅下线：键立即消失
```

## guild↔game 连接层（公会服务）

- **方向**：game = `tcp.Connector`（`guildclient.Pool`，1s 重连），guild = `tcp.Acceptor`（8903）；地址来自 `registry` 发现 guild（`ServiceGuild`）或回退 `config.Game.GuildServers`。gate 不参与公会链路。
- **握手**：game 发 `RegisterGame{game_id,ts,nonce,hmac}`（`hmac=base64(HMAC-SHA256(guildSecret, game_id|ts|nonce))`），guild 校验后回 `RegisterAck`；`guildSecret` 空 = 不校验（仅联调）。
- **上行**：客户端公会消息经 gate 默认转发到 game → `game/handler/guild.go` 校验 `OnlineRole` 并填 `uid/role_id/role_name/role_level` → 组 `Transmit{uid,role_id,role_name,role_level,msg_id,payload}` → 轮询已就绪 guild 连接；guild 的 svcproc 解包后直接按最终协议分发。
- **下行**：guild 回 `GuildS2C{uid,msg_id,payload}` → game 经 `ForwardS2C` 回客户端；推送 `GuildNotify{role_id,msg_id,payload}` 广播给所有已握手 game，game 用 `FindOnlineByRole` 命中在线角色后转发。
- **权限**：会长=任免副会长/加入开关/转让/解散/踢任何人；副会长=邀请/改公告/踢普通成员；会长不可退出/被踢（须转让或解散）；转让后旧会长降为成员。
- **容量**：`members.role_id` 唯一（一角色一公会）+ `member_count<maxMembers` 条件 `$inc`；先插成员，自增未命中则回滚并回 `guild is full`。
- **失败语义**：game 启动时 guild 不可用不致命（操作本地回 `guild unavailable`）；guild 自身 Mongo/Redis 不可用 fail-fast。

## gate↔game 连接层

- **方向**：gate = `tcp.Connector`（主动连 game，`SetReconnectDuration(1s)` 自动重连），game = `tcp.Acceptor`；地址来自 `registry` 发现 game 服务（`registry.endpoints` 非空时纯 etcd watch），否则回退 `config.Gate.GameServers` 静态列表。
- **握手**：连接成功后 gate 发 `RegisterGate{gate_id}`，game 回 `RegisterAck`。
- **路由**：登录时 `GamePool.Assign(uid)` RoundRobin 选**已连通**实例，`uid→addr` 存 GamePool；重连后按路由补发 `PlayerOnline`（game 状态已丢，仅补登记在线表）。
- **登录握手**（客户端真正登录时才走）：gate `handleLogin` → `Assign` → `PlayerLogin{uid,account,ip,version}` → game `UserManager.Login`（Mongo upsert + 登记在线）→ `PlayerLoginAck{code,reason,roles}` → gate `onPlayerLoginAck` 回投 `LoginAck`（含 `roles`）给客户端；失败（code≠0）时 gate 回滚路由与登录态。**新账号 roles 为空，登录不建角色**。**game 不回包则客户端一直等待**（`TODO` 登录超时）。
- **角色与进入游戏**（登录后经 gate 默认转发）：`CreateRoleReq{name}` → game 取全局 roleId（Redis INCR）→ 插 `roles`（`account` 归属）→ `CreateRoleAck`；`EnterGameReq{role_id}` → 按 `account` 加载角色完整数据校验归属 → **仅内存**置 `OnlineRole` → `EnterGameAck`。gate 不解析业务，只封 `Transmit`/回投 `ForwardS2C`。
- **玩家生命周期** `PlayerOnline/PlayerOffline`：reason 0=主动退出 1=断线 2=顶号迁移到其他实例（旧实例下线）；`PlayerLogin` 成功即等价于上线登记。
- **业务转发**：gate 未处理的消息 → `Transmit{uid, msg_id, payload}`（信封不解析业务）→ `core/svcproc` 解包为最终协议 → game 按注册类型处理 → `ForwardS2C` 回包 → gate 查 `SessionByUID` 发 `RawPacket` 给客户端。
- 所有 gate 事件（客户端 + game 连接）共用**同一个 EventQueue** 串行处理（`net.Init` 创建，ws 与对内连接都从 `net.Queue()` 取）。

## core 装配与注册（启动约定）

- **启动顺序**：`net.Init` → `Register`（藏在 `backend.Pool.Start`/`frontend.Server.Start` 开头，由 `gate/app.go` 的 `run` 调用）→ 建 peer → `net.Run`/`net.RunContext`。事件在 `Run` 前只入队不派发（`SessionQueuedCall` → `queue.Post`），所以先起后端连接池、后起前端监听、handler 注册晚于 peer 启动也安全。
- **统一 dispatch**：Listen/Connect 内部与 gate ws 装配（`net.Bind`）共用同一个处理函数；系统事件（实现 `SystemMessage()`）按**类型名**（`"SessionClosed"`）查表，业务消息按 **meta.ID** 查表。
- **协议名注册**：`Register("gate.X", h)` 经 `util.StringHash` → `MessageMetaByID` 归一到消息 ID（cellnet 的 `FullName()` 是 Go 包名 `proto.X`，与 proto 包名 `gate.X` 不同，两条路都试）。
- **SessionClosed 分流**：gate 进程里 ws 客户端与 game 连接都会触发全局 key `"SessionClosed"`，两个 handler 都注册、各自按 `ev.Session().Peer().TypeName()`（`gorillaws.Acceptor` / `tcp.Connector`）过滤；game 进程同时有 gate Acceptor 与 guild Connector，`game/handler/gate.go` 与 `game/guildclient/handler.go` 也都按类型过滤。
- **默认兜底**：`RegisterDefault` 只对业务消息生效（gate 用它做未注册消息的 C2S 转发），系统事件未注册不走兜底。

## 登录态与广播（gate）

- 状态在 `gate/session.Manager`：`byUID`（uid→LoginInfo）+ `bySession`（sessionID→uid），断线 `SessionClosed` 清理（幂等）。
- **顶号**：同 uid 重复登录 → 旧会话收 `KickNtf` 后 Close，新会话接管；`RemoveBySession` 只删仍指向该会话的记录，不误删新登录态。
- **广播统一入口** `Manager.BroadcastToUids(uids, msg)`：uids 空=全局（仅已登录会话），非空=按列表——预留 game 服务消息驱动；先取快照再 Send，锁外做网络 IO。
- `BroadcastReq` 是**联调后门**（客户端可触发广播），仅 `auth.mode=dev` 注册；生产（jwt）不暴露。

## 鉴权

- **客户端**：登录时 `VerifyToken` 校验 token，uid/account 以校验结果为准（不信任客户端自报 uid）。`auth.mode=dev` 保留占位校验（account/uid/token 非空），`jwt` 用 HS256 本地验签并校验 `exp`（依赖 `github.com/golang-jwt/jwt/v5`）。`auth.tokenTTL>0` 时会话过期，业务消息经 `requireLogin` 刷新 `Touch`，过期踢下线。
- **登录守卫**：`requireLogin(ev)` 用于 `forwardC2S`/`onBroadcast`，未登录或过期直接丢弃。
- **内网**：gate 连上 game 发 `RegisterGate{gate_id,ts,nonce,hmac}`，`hmac=base64(HMAC-SHA256(gateSecret, gate_id|ts|nonce))`；game 校验时间窗（±30s）+ constant-time 比较，失败回 `code=1` 并 Close。`gateSecret` 为空=不校验（仅联调），两侧须配相同值。

## 服务注册与发现（etcd）

- **键布局**：`{prefix}/services/{service}/{id}` → `{"addr":"...","meta":{...}}`；服务名常量 `registry.ServiceGate`/`ServiceGame`/`ServiceGuild`。
- **配置**：各服务 `registry.endpoints` 非空即启用（默认 `prefix=/mgame`、`ttl=10s`）；`endpoints` 空 = 不启用——gate 回退静态 `gameServers`，game 回退静态 `guildServers`，game/guild 不注册。
- **注册时机**：都在**监听启动之后**注册（game 在 `net.Listen` 后、gate 在 `p.Start()` 后），确保别人拿到地址即可拨号；`GameID`/`GateID` 为空自动生成 `<service>-<pid>`，`advertise` 为空回退 `listen`（多机部署必须配，`0.0.0.0` 注册出来拨不通）。
- **租约**：`Grant+Put(WithLease)+KeepAlive` 后台续约；续约通道关闭（etcd 不可达/租约过期）自动重新 Grant+Put；进程崩溃由 ttl 自动清理。
- **优雅下线**：main 用 `net.SignalContext()`（SIGINT/SIGTERM）→ ctx 取消 → `net.RunContext` 停事件循环 → `defer` 执行 `Registration.Close()`（删键，`gate/app.go` 的 `run` 内）与 `net.CloseRegistry()`。**`net.Run` 无此语义，各服务已改用 `RunContext`。**
- **gate 侧发现**：`core/discover` 的 `Registry` 包装 `reg.Watch("game")`（latest-wins 快照），watch 启动失败（etcd 不可达）回退静态地址；`backend.Pool.reconcile` 只做**增删 connector**，不做玩家迁移——game 消失后其路由玩家需重连重登。
- **game 侧发现 guild**：`discover.NewRegistry(reg, registry.ServiceGuild, discover.Static(cfg.GuildServers))`，`guildclient.Pool.reconcile` 同理只增删 connector（公会数据在 Mongo，重连后无需补状态）。
- **已知坑**：`gate/app.go` 的 `run` 必须先 `frontend.Configure(cfg.Auth)` + `backend.SetSecret(cfg.GateSecret)` 再建连接池（漏调 → `gateSecret` 为空 → `RegisterGate` 无签名 → game 全部拒连并 1s 重连风暴）。

## cellnet 诡异点（踩过的坑）

- 框架：`github.com/davyxu/cellnet@v4.1.0`（+incompatible，无 go.mod）。
- 必须 blank import 才能注册 peer/proc/codec：`peer/tcp`、`codec/gogopb`、`_ github.com/zhenhuasu/mgame/core/svcproc`（注册 `"svc.backend"`，替代 `proc/tcp`）、`_ github.com/zhenhuasu/mgame/proto` 已集中在 `core/net/init.go`；gate 另需 `peer/gorillaws` + `_ github.com/zhenhuasu/mgame/gate/frontend/wproc`（注册 `"gate.ws"`，替代 `proc/gorillaws`，在 `gate/frontend/server.go`）。tcp peer 依赖 `github.com/davyxu/protoplus`（已在 go.mod）。
- **WS/TCP 帧格式**：2 字节小端 msgID + protobuf body；msgID 是 **uint16**（`util.StringHash("proto包名.消息名")`），注册时 ID=0 或重复会 panic。
- cellnet 没有 `cellnet.Connector` 接口，tcp connector 断言用 `cellnet.TCPConnector`（含 `Session()`/`SetReconnectDuration`）；peer 名取 `ev.Session().Peer().(cellnet.PeerProperty).Name()`（Peer 接口本身无 Name）。
- tcp connector 的 `SessionConnected` 每次重连都会触发——`RegisterGate`/补发 `PlayerOnline` 挂在该事件上即可。
- wss 由 `WSAcceptor.SetHttps(certFile, keyFile)` 切换（`config.protocol: wss` 时调用）；地址需 `scheme://host:port/path` 格式（`config.Gate.Addr()` 已拼好）。
- cellnet 自带 ws connector 默认不信任自签证书；wss 客户端测试需自行设置 `TLSClientConfig.InsecureSkipVerify` 或用受信证书。
- `protoc-gen-msg` 的 `--msg_out` 参数必须是**输出文件名**（如 `msgid.go:.`），传目录会报 "First file chunk..." 错误；且它会为请求内**所有** `.proto`（含被 import 的）生成注册，因此本项目只用 `inner.proto`（import 了 `gate.proto`）生成一份 `proto/msgid.go`。
- cellnet 日志走 `davyxu/golog`，**同名 logger 重复注册会 panic**，包级 logger 须唯一；按名注册、默认 debug；`golog.SetLevelByString` 的 name 是**正则子串**，`"gorillaws"` 同时命中 `gorillaws`+`gorillawsproc`，`"gate"` 同时命中 `gate`+`gateproc`+`gate.frontend`+`gate.backend`，`"game"` 命中 `game`+`game.handler`+`game.network`+`game.service`+`game.repo`+`game.guildclient`，`"guild"` 命中 `guild`+`guild.handler`+`guild.network`+`guild.service`+`guild.repo`（含 `mongo`/`redis` 子串无须单独设），`"tcp"` 命中 `tcppeer`（gate/game 已不注册 `tcpproc`，proc 由 `svcproc`/`wproc` 替代）。配置项 `logLevel`（debug/info/warn/error）在 `net.Init` 开头统一设置（经 `InitOptions.LogNames`），设 `info` 即可关掉收发消息 dump。

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
- 已 `git init`（远端 `github.com/zhenhuasu/mgame`，分支 `main`）。
- 尚无 lint / typecheck / test 命令；新增工具链后必须同步更新本文件。
- 文档与代码冲突时，以代码为准。
