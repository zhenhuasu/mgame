# mgame

Go 分布式游戏服务器骨架：**gate（网关）+ game（游戏服）+ guild（公会服）+ rank（排行榜服）四服务**，支持 etcd 服务注册发现、JWT 客户端鉴权、gate↔game / game↔guild / gate·game↔rank HMAC 内网握手。

- 模块路径：`github.com/zhenhuasu/mgame`
- 单 Go 模块，`go build ./...` 即可运行；**用户/角色持久化在 MongoDB（users/roles 两集合），公会持久化在同一库的 guilds/guild_members/guild_invites 三集合；排行榜用 Redis ZSET（不落 Mongo）；Redis 另做读缓存与全局 roleId/guildId 自增**，game/guild 依赖 Mongo+Redis，rank 只依赖 Redis

## 目录结构

```
mgame/
├── core/                # 跨服务公共能力
│   ├── net/             # cellnet 装配、事件队列、统一消息分发、注册中心托管
│   ├── svcproc/         # 后端通信自定义 proc（LTV 帧 + Transmit 解包）
│   ├── auth/            # 内网 HMAC-SHA256 签名/校验（SignPeer/VerifyPeer）
│   ├── discover/        # 通用实例发现（静态 / etcd watch，gate→game、game→guild、gate/game→rank）
│   ├── registry/        # 服务注册与发现（etcd / 内存静态实现）
│   ├── redis/           # Redis 连接与 get/set/del/incr（fail-fast）
│   └── mongo/           # MongoDB 连接 + 泛型 CRUD/索引助手（fail-fast）
├── gate/                # 网关服务：ws/wss 接入客户端，tcp 连 game/rank
│   ├── main.go          # 入口：配置 + net.Init
│   ├── app.go           # 装配：前端/后端/rank/登录态接线 + 自注册 + 事件循环
│   ├── frontend/        # 与客户端通信：ws 装配、客户端 handler、token 鉴权、RankListReq
│   ├── backend/         # 与 game 通信：连接池、实例发现（core/discover）、内网 HMAC 握手
│   ├── rankclient/      # gate→rank 连接池、握手、RankListReq RPC
│   └── session/         # 共享登录态（顶号 / 过期 / 广播）
├── game/                # 游戏服务：用户/角色、在线管理、业务
│   ├── main.go          # 入口：配置 + net.Init + Mongo/Redis/仓储/管理器装配
│   ├── handler/         # 消息处理：gate 握手、登录/生命周期、角色进入、业务转发
│   ├── repo/            # 仓储：Mongo 两集合 + Redis 读缓存装饰器 + 唯一索引
│   ├── service/         # 业务：UserManager（在线/角色/进入游戏）、RoleIDGen（Redis INCR）
│   ├── model/           # 领域结构（User / Role）
│   ├── guildclient/     # game→guild 连接池、握手、GuildS2C/Notify 下行投递
│   ├── rankclient/      # game→rank 连接池、握手、RankUpdate 上报、RankChangeNotify
│   └── network/         # 传输装配（cellnet Listen + 自注册）
├── guild/               # 公会服：独立进程，game 连入，Mongo 唯一持久层
│   ├── main.go          # 入口：配置 + net.Init + Mongo/Redis/仓储/管理器装配
│   ├── handler/         # RegisterGame 握手、公会最终协议分发（Transmit 已解包）、GuildS2C/Notify
│   ├── repo/            # guilds / guild_members / guild_invites + 唯一索引
│   ├── service/         # GuildManager（权限/容量/邀请）、GuildIDGen（Redis INCR）
│   ├── model/           # 领域结构（Guild / GuildMember / GuildInvite）
│   └── network/         # 传输装配（cellnet Listen + 自注册）
├── rank/                # 排行榜服：独立进程，Redis ZSET 唯一存储
│   ├── main.go          # 入口：配置 + net.Init + Redis/仓储/管理器装配
│   ├── handler/         # RegisterRank 握手、RankUpdate、RankListReq RPC、RankChanged 广播
│   ├── repo/            # Redis ZSET + profile Hash + 原子 Lua 更新
│   ├── service/         # RankManager（分页/校验/排名变化通知）
│   ├── model/           # Board / Profile / Entry / UpdateMode
│   └── network/         # 传输装配（cellnet Listen + 自注册）
├── client/              # 测试客户端（Ping / 登录 / 广播 / Hello / 公会 / 排行榜）
├── proto/               # protobuf（gate.proto 客户端协议 + inner.proto 内网协议）
├── config/              # 配置加载与各服务 yaml
└── AGENTS.md            # 项目约定与实现细节
```

## 快速开始

需要 Go 1.26+、本机 MongoDB（27017）与 Redis（6379）；game/guild 依赖两者，rank 只依赖 Redis，缺失时对应服务启动失败。

```bash
go build ./...
go vet ./...

# 0. 起排行榜服（默认 127.0.0.1:8904；需 Redis）
go run ./rank -config config/rank.yaml

# 1. 起公会服（默认 127.0.0.1:8903；可选，game 启动后连它）
go run ./guild -config config/guild.yaml

# 2. 起游戏服（默认 127.0.0.1:8901；需本机 MongoDB + Redis）
go run ./game -config config/game.yaml
# 第二个实例（负载均衡用，127.0.0.1:8902）
go run ./game -config config/game2.yaml

# 3. 起网关（ws://0.0.0.0:8801/ws）
go run ./gate -config config/gate.yaml

# 4. 测试客户端
go run ./client -addr ws://127.0.0.1:8801/ws                          # Ping
go run ./client -account a -uid 1001 -token t -version v1.0.0          # 登录（打印角色）+ 自动建角色并进入
go run ./client -account a -uid 1001 -token t -flow enter -role 3      # 直接进入指定角色
go run ./client -account a -uid 1001 -token t -flow create -role-name b # 强制新建角色并进入
go run ./client -account a -uid 1001 -token t -broadcast -content hi   # 全局广播
go run ./client -account a -uid 1001 -token t -hello -content hi       # Hello 经 gate→game→gate 回包

# 公会（角色须先 EnterGame）
go run ./client -account a -uid 1001 -token t -flow create -guild create -guild-name G1
go run ./client -account b -uid 1002 -token t -flow create -guild join -guild-id 1
go run ./client -account a -uid 1001 -token t -guild info
go run ./client -account a -uid 1001 -token t -guild invite -target-name b
go run ./client -account b -uid 1002 -token t -guild respond -guild-id 1 -accept
go run ./client -account a -uid 1001 -token t -guild set-position -target-role 29 -position vice
go run ./client -account a -uid 1001 -token t -guild transfer -target-role 29
go run ./client -account b -uid 1002 -token t -guild disband

# 排行榜（角色须先 EnterGame；power|level）
go run ./client -account a -uid 1001 -token t -flow enter -role 3 -rank list -rank-board power -rank-limit 10
go run ./client -account a -uid 1001 -token t -rank list -rank-board level -rank-offset 0 -rank-limit 20

mongosh mgame --quiet --eval 'db.roles.find().toArray()'               # 角色集合
mongosh mgame --quiet --eval 'db.guilds.find().toArray()'              # 公会
mongosh mgame --quiet --eval 'db.guild_members.find().toArray()'       # 公会成员
mongosh mgame --quiet --eval 'db.guild_invites.find().toArray()'       # 公会邀请
redis-cli get mgame:guild:id                                            # 全局公会 ID 计数器
redis-cli get mgame:user:a                                             # 用户读缓存（登录/写后失效）
redis-cli get mgame:role:id                                            # 全局角色 ID 计数器
redis-cli ZREVRANGE mgame:rank:power 0 9 WITHSCORES                     # 战力榜
redis-cli ZREVRANGE mgame:rank:level 0 9 WITHSCORES                     # 等级榜
redis-cli HGETALL mgame:rank:profile                                    # 榜单展示信息
```

> 构建二进制请输出到 `bin/`（`go build -o bin/ ./gate ./game ./guild ./rank`），`/bin/` 已被 gitignore。

## 配置

各服务配置在 `config/*.yaml`：

| 字段 | 说明 |
|---|---|
| `listen` / `path` / `protocol` | 监听地址与协议（`ws` / `wss`） |
| `auth.mode` | `dev`（占位校验）或 `jwt`（HS256 本地验签，需 `jwtSecret`） |
| `auth.tokenTTL` | 登录会话有效期（秒） |
| `gateSecret` | gate↔game 内网 HMAC 共享密钥，空 = 不校验（仅联调） |
| `guildServers` | game 连接的 guild 地址列表（未启用注册中心时用），空 = 无公会功能 |
| `guildSecret` | game↔guild 内网 HMAC 共享密钥，空 = 不校验（仅联调） |
| `guild.maxMembers` | 公会成员上限，默认 50 |
| `guild.nameMaxLen` / `noticeMaxLen` | 公会名/公告最大长度，默认 24/200 |
| `rankServers` | gate/game 连接的 rank 地址列表（未启用注册中心时用），空 = 无排行榜功能 |
| `rankSecret` | gate/game↔rank 内网 HMAC 共享密钥，空 = 不校验（仅联调） |
| `role.expBase` / `role.powerPerLevel` | 升级所需经验基数、每级战力增量，默认 100/100 |
| `role.maxLevel` | 角色最高等级，默认 100 |
| `rank.defaultLimit` / `rank.maxLimit` | rank 查询默认/最大条数，默认 20/100 |
| `advertise` | 注册到 etcd 的对外地址，空 = 回退 `listen` |
| `registry.endpoints` | etcd 地址，**非空即启用**；空 = 不启用，gate 回退静态 `gameServers` |
| `registry.prefix` | etcd 键前缀，空 = `/mgame` |
| `registry.ttl` | 租约秒数，默认 10s |
| `mongo.uri` | MongoDB 连接串，**空 = game/guild 启动失败** |
| `mongo.database` | 数据库名，空 = `mgame` |
| `redis.addr` | 各服务共用的 Redis 地址，**空 = game/guild 启动失败** |
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
7. 角色成长：`Role` 含 `avatar/exp/power`；`GainExp` 按 `expBase*当前等级` 扣经验并升级、每级 `power += powerPerLevel`，`maxLevel` 封顶后经验清零；进入游戏/升级/改头像后异步更新 power/level 两个榜单。

### 持久化结构（MongoDB `mgame` 库）

| 集合 | 唯一索引 | 关键字段 |
|---|---|---|
| `users` | `account` | `uid` `ip` `version` `created_at` `updated_at`（**不存角色/在线角色**） |
| `roles` | `role_id`、`name`（全局唯一） | `role_id`(全局) `account` `name` `level` `avatar` `exp` `power` `created_at` `updated_at` |
| `guilds` | `guild_id`、`name` | `guild_id` `name` `leader_role_id` `notice` `level` `join_mode` `member_count` `created_at` `updated_at` |
| `guild_members` | `role_id`、`(guild_id,role_id)` | `guild_id` `role_id` `role_name` `role_level` `position` `joined_at` |
| `guild_invites` | `(guild_id,role_id)` | `guild_id` `role_id` `invited_by` `invited_by_name` `created_at` |

`users` 不存角色列表：登录时按 `roles.account` 反查派生内存 `RoleIDs`；`OnlineRole` 仅进程内存。公会侧 `guild_members.role_id` 唯一 = 一角色一公会，成员与公会计数分离（`member_count` 由条件 `$inc` 维护）。Redis 键：`mgame:user:<account>`（读缓存）、`mgame:role:id`（角色 ID 自增）、`mgame:guild:id`（公会 ID 自增）、`mgame:rank:<board>`（排行榜 ZSET）、`mgame:rank:profile`（角色展示信息 Hash）。

## 公会服务

1. **拓扑**：客户端公会消息仍走 `client → gate → game`（gate 零改动，默认转发未注册消息）；game 校验 `OnlineRole` 后组 `Transmit{uid,role_id,role_name,role_level,msg_id,payload}` 经 `guildclient` 发往 guild（guild 侧 svcproc 解包为最终协议）；guild 回 `GuildS2C` 经 game `ForwardS2C` 回客户端。
2. **功能**：创建、直接加入（`join_mode=open|invite_only` 由会长开关控制）、邀请/查看邀请/接受或拒绝、退出、踢人、转让会长、任免副会长、改公告、公会列表/信息/成员列表、解散。会长不可退出/被踢；转让后旧会长降为普通成员。
3. **权限**：会长=一切；副会长=邀请/改公告/踢普通成员；加入开关/任免/转让/解散仅会长。
4. **容量**：先插 `guild_members`（`role_id` 唯一）再 `guilds.member_count<maxMembers` 条件 `$inc`，未命中回滚并回 `guild is full`——无事务下也能防超员。
5. **推送**：邀请到达推 `GuildInviteNtf`；被踢/解散/转让/职位变更推 `GuildEventNtf`（guild 广播 `GuildNotify` 给所有已握手 game，game 按在线角色命中后转发）。
6. **失败语义**：game 启动时 guild 不可用不致命（公会操作本地回 `guild unavailable`）；guild 自身 Mongo/Redis 不可用 fail-fast。

## 排行榜服务

1. **拓扑**：gate/game 各自用 `tcp.Connector` 连 rank（默认 8904），发 `RegisterRank{client_id,kind,ts,nonce,hmac}`（`kind=gate|game`）；rank 回 `RegisterRankAck`。客户端查询走 `client → gate → rank`，gate 用 cellnet RPC（`RankListReq/RankListAck`），不经过 game。
2. **写入**：game 在 `EnterGame`、`SetAvatar`、升级（`GainExp` 触发）后异步 `Transmit` 两条 `RankUpdate`（power/level，覆盖模式）；rank 用 Lua 原子完成旧排名→`ZADD`/`ZINCRBY`→新排名→写 profile。也支持 `RANK_UPDATE_DELTA` 增量模式。
3. **查询**：`RankListReq{board,offset,limit,role_id}`，limit 默认 20、上限 100；`RankListAck` 返回 1-based 排名、总分、总人数与自身排名。gate 只允许已登录会话查询，rank 不可用时回 `code=1`。
4. **成长**：升到下一级需 `expBase*当前等级` 经验；每次升级 `power += powerPerLevel`；`maxLevel` 封顶后经验清零。角色新增 `avatar/exp/power`，经验增加与头像设置目前仅服务端内部接口，不暴露客户端协议。
5. **失败语义**：rank 不可用不影响 game/gate 启动；上报失败仅日志，客户端查询快速失败。rank 只依赖 Redis，自身 Redis 不可用 fail-fast。

## 服务注册与发现

启用 `registry.endpoints` 后：

- game 监听就绪后注册 `/mgame/services/game/<id>`，gate 注册 `/mgame/services/gate/<id>`，guild 注册 `/mgame/services/guild/<id>`，rank 注册 `/mgame/services/rank/<id>`
- gate 通过 etcd watch 发现 game；game 发现 guild 与 rank；gate 也发现 rank；未启用时分别回退静态 `gameServers` / `guildServers` / `rankServers`
- `SIGINT` / `SIGTERM` 优雅下线：注销自身键后再退出

```bash
etcdctl get --prefix /mgame/services/            # 查看注册结果
```

## 鉴权

1. **客户端 → gate**：`auth.mode=dev` 时 token 为占位；`jwt` 时本地 HS256 验签，`uid` / `account` 以 token 为准。未登录消息一律拒绝。
2. **gate → game**：`RegisterGate` 携带 `gate_id|ts|nonce` 的 HMAC-SHA256 签名（30s 时间窗），game 校验后才接受该会话的业务消息。
3. **game → guild / gate·game → rank**：`RegisterGame`/`RegisterRank` 携带 `client_id|ts|nonce` HMAC（`guildSecret`/`rankSecret`），对端校验后才接受请求。均复用 `core/auth.SignPeer`/`VerifyPeer`。
4. JWT 测试需在客户端传 `-jwt-secret`，与 `auth.jwtSecret` 一致。

## 开发约定

详见 [AGENTS.md](./AGENTS.md)。要点：

- 改 `.proto` 后运行 `./proto/gen.sh` 重新生成，勿手改 `*.pb.go` / `msgid.go`
- `gate.proto` 与 `inner.proto` 必须同属 proto package `gate`；msgid 只由 `inner.proto` 生成一份 `proto/msgid.go`
- 提交前：`gofmt -l .`、`go build ./...`、`go vet ./...`、`go test ./...`

## License

[MIT](./LICENSE)
