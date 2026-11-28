// gate/client 测试客户端：Ping 回环 / 登录-广播-退出 / 收 KickNtf、NotifyNtf
package main

import (
	"flag"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/davyxu/cellnet"
	"github.com/davyxu/cellnet/peer"
	"github.com/davyxu/cellnet/proc"
	"github.com/davyxu/golog"
	"github.com/golang-jwt/jwt/v5"

	_ "github.com/davyxu/cellnet/codec/gogopb"
	_ "github.com/davyxu/cellnet/peer/gorillaws"
	_ "github.com/davyxu/cellnet/proc/gorillaws"

	pb "github.com/zhenhuasu/mgame/proto"
)

var log = golog.New("client")

// 事件流：handler → 主流程按类型等待
type evt struct {
	kind string // connected/loginack/logoutack/broadcastack/notify/kick/closed/pingack/connecterr/createroleack/entergameack
	raw  interface{}
}

func main() {
	addr := flag.String("addr", "ws://127.0.0.1:8801/ws", "ws:// 或 wss:// 地址")
	account := flag.String("account", "", "账号（空=仅 Ping 测试）")
	uid := flag.Int64("uid", 0, "uid（>0 触发登录流程）")
	token := flag.String("token", "", "token")
	jwtSecret := flag.String("jwt-secret", "", "非空则本地签发 HS256 token（覆盖 -token）")
	version := flag.String("version", "1.0.0", "客户端版本号（随登录上报）")
	broadcast := flag.Bool("broadcast", false, "登录后触发一次广播")
	uidsFlag := flag.String("uids", "", "广播 uid 列表，逗号分隔；空=全局")
	content := flag.String("content", "hello", "广播内容")
	logout := flag.Bool("logout", false, "广播后发送退出")
	hello := flag.Bool("hello", false, "登录后发送 HelloReq（经 gate→game 全链路）")
	roleID := flag.Int64("role", 0, "进入游戏的角色 id（0=按 -flow 自动处理；-flow enter 时 0 原样交服务端默认处理）")
	flow := flag.String("flow", "", "进入流程：create=先建角色；enter=按 -role 直接进入；空=有角色进入/无角色先建")
	roleName := flag.String("role-name", "", "CreateRole 角色名（空=账号名）")
	wait := flag.Duration("wait", 2*time.Second, "登录/广播后等待通知的时长")
	guildOp := flag.String("guild", "", "公会操作: create|join|leave|kick|transfer|notice|disband|info|list|invite|invites|respond|set-join-mode|set-position")
	guildName := flag.String("guild-name", "", "创建公会名")
	guildID := flag.Int64("guild-id", 0, "公会 id（join/info/respond/set-* 用；info 0=自己所在公会）")
	targetRole := flag.Int64("target-role", 0, "目标角色 id（kick/transfer/invite/set-position）")
	targetName := flag.String("target-name", "", "目标角色名（invite 且 -target-role=0 时按名解析）")
	notice := flag.String("notice", "", "公告内容（notice）")
	joinMode := flag.String("join-mode", "", "加入方式: open|invite_only")
	position := flag.String("position", "", "职位: vice|member")
	accept := flag.Bool("accept", false, "响应邀请：true=接受，false=拒绝")
	rankOp := flag.String("rank", "", "排行榜操作: list")
	rankBoard := flag.String("rank-board", "power", "榜单: power|level")
	rankOffset := flag.Int("rank-offset", 0, "榜单偏移")
	rankLimit := flag.Int("rank-limit", 20, "榜单条数")
	flag.Parse()

	queue := cellnet.NewEventQueue()
	p := peer.NewGenericPeer("gorillaws.Connector", "client", *addr, queue)

	events := make(chan evt, 64)
	// done 由外部关闭，handler 内 once 保护
	done := make(chan struct{})

	proc.BindProcessorHandler(p, "gorillaws.ltv", func(ev cellnet.Event) {
		switch msg := ev.Message().(type) {
		case *cellnet.SessionConnected:
			events <- evt{"connected", nil}

		case *pb.LoginAck:
			events <- evt{"loginack", msg}

		case *pb.LogoutAck:
			events <- evt{"logoutack", msg}

		case *pb.BroadcastAck:
			events <- evt{"broadcastack", msg}

		case *pb.NotifyNtf:
			log.Infof("NotifyNtf: %q", msg.Content)
			events <- evt{"notify", msg}

		case *pb.KickNtf:
			log.Warnf("KickNtf: %q", msg.Reason)
			events <- evt{"kick", msg}

		case *pb.PingAck:
			log.Infof("PingAck session_id=%d echo_ts=%d", msg.SessionId, msg.Timestamp)
			events <- evt{"pingack", msg}

		case *pb.HelloAck:
			log.Infof("HelloAck content=%q", msg.Content)
			events <- evt{"helloack", msg}

		case *pb.CreateRoleAck:
			events <- evt{"createroleack", msg}

		case *pb.EnterGameAck:
			events <- evt{"entergameack", msg}

		case *pb.CreateGuildAck:
			events <- evt{"createguildack", msg}
		case *pb.JoinGuildAck:
			events <- evt{"joinguildack", msg}
		case *pb.LeaveGuildAck:
			events <- evt{"leaveguildack", msg}
		case *pb.KickGuildMemberAck:
			events <- evt{"kickguildack", msg}
		case *pb.TransferLeaderAck:
			events <- evt{"transferguildack", msg}
		case *pb.SetGuildNoticeAck:
			events <- evt{"noticeguildack", msg}
		case *pb.SetJoinModeAck:
			events <- evt{"joinmodeguildack", msg}
		case *pb.SetMemberPositionAck:
			events <- evt{"positionguildack", msg}
		case *pb.DisbandGuildAck:
			events <- evt{"disbandguildack", msg}
		case *pb.GetGuildInfoAck:
			events <- evt{"infoguildack", msg}
		case *pb.ListGuildsAck:
			events <- evt{"listguildsack", msg}
		case *pb.InviteMemberAck:
			events <- evt{"inviteguildack", msg}
		case *pb.ListInvitesAck:
			events <- evt{"invitesguildack", msg}
		case *pb.RespondInviteAck:
			events <- evt{"respondguildack", msg}

		case *pb.RankListAck:
			events <- evt{"ranklistack", msg}

		case *pb.GuildInviteNtf:
			log.Infof("GuildInviteNtf guild=%d %q by=%d(%q)", msg.GuildId, msg.GuildName, msg.InvitedBy, msg.InvitedByName)
			events <- evt{"guildinvitentf", msg}
		case *pb.GuildEventNtf:
			log.Infof("GuildEventNtf kind=%d guild=%d %q position=%d operator=%d", msg.Kind, msg.GuildId, msg.GuildName, msg.Position, msg.OperatorRoleId)
			events <- evt{"guildeventntf", msg}

		case *cellnet.SessionClosed:
			log.Infof("closed: %v", msg.Reason)
			events <- evt{"closed", msg}

		case *cellnet.SessionConnectError:
			log.Errorf("connect error")
			events <- evt{"connecterr", nil}
			close(done)
		}
	})

	p.Start()
	queue.StartLoop()

	exit := func(code int) {
		p.(cellnet.GenericPeer).Stop()
		queue.StopLoop()
		os.Exit(code)
	}

	// 等待指定事件类型；找到返回 (payload, true)，断开/连接失败/超时返回 (nil, false)
	waitFor := func(kind string, timeout time.Duration) (interface{}, bool) {
		deadline := time.After(timeout)
		for {
			select {
			case e := <-events:
				if e.kind == kind {
					return e.raw, true
				}
				if e.kind == "closed" || e.kind == "connecterr" {
					return nil, false
				}
				// 非目标事件丢弃（notify/kick 已由 handler 打日志）
			case <-deadline:
				return nil, false
			case <-done:
				return nil, false
			}
		}
	}

	// 1. 连接
	if _, ok := waitFor("connected", 5*time.Second); !ok {
		log.Errorf("connect timeout")
		exit(1)
	}
	log.Infof("connected")

	// 2. Ping 回环（无登录参数时只做这个）
	if *uid <= 0 {
		connectorSession(p).Send(&pb.PingReq{Timestamp: time.Now().UnixMilli()})
		if _, ok := waitFor("pingack", 3*time.Second); !ok {
			log.Errorf("ping timeout")
			exit(1)
		}
		time.Sleep(100 * time.Millisecond)
		exit(0)
	}

	// 3. 登录（-jwt-secret 非空时本地签发 token）
	conn := connectorSession(p)
	loginToken := *token
	if *jwtSecret != "" {
		t, err := signJWT(*jwtSecret, *uid, *account)
		if err != nil {
			log.Errorf("sign jwt: %v", err)
			exit(1)
		}
		loginToken = t
	}
	conn.Send(&pb.LoginReq{Account: *account, Uid: *uid, Token: loginToken, Version: *version})
	rawAck, ok := waitFor("loginack", 3*time.Second)
	if !ok {
		log.Errorf("login ack timeout")
		exit(1)
	}
	ack := rawAck.(*pb.LoginAck)
	if ack.Code != 0 {
		log.Errorf("login failed code=%d reason=%q", ack.Code, ack.Reason)
		exit(1)
	}
	log.Infof("login ok uid=%d roles=%d", ack.Uid, len(ack.Roles))
	for _, r := range ack.Roles {
		log.Infof("  role id=%d name=%q level=%d", r.RoleId, r.Name, r.Level)
	}

	// 3.5 进入游戏：-flow 指定流程，空 = 有角色直接进 / 无角色先建
	// send+wait 两个工具函数
	createRole := func() *pb.RoleInfo {
		conn.Send(&pb.CreateRoleReq{Name: *roleName})
		raw, ok := waitFor("createroleack", 3*time.Second)
		if !ok {
			log.Errorf("create role ack timeout")
			exit(1)
		}
		crAck := raw.(*pb.CreateRoleAck)
		if crAck.Code != 0 {
			log.Errorf("create role failed code=%d reason=%q", crAck.Code, crAck.Reason)
			exit(1)
		}
		log.Infof("create role ok id=%d name=%q level=%d", crAck.Role.RoleId, crAck.Role.Name, crAck.Role.Level)
		return crAck.Role
	}
	enterGame := func(id int64) *pb.RoleInfo {
		conn.Send(&pb.EnterGameReq{RoleId: id})
		raw, ok := waitFor("entergameack", 3*time.Second)
		if !ok {
			log.Errorf("enter game ack timeout")
			exit(1)
		}
		egAck := raw.(*pb.EnterGameAck)
		if egAck.Code != 0 {
			log.Errorf("enter game failed code=%d reason=%q", egAck.Code, egAck.Reason)
			exit(1)
		}
		log.Infof("enter game ok role_id=%d name=%q level=%d exp=%d power=%d",
			egAck.Role.RoleId, egAck.Role.Name, egAck.Role.Level, egAck.Role.Exp, egAck.Role.Power)
		return egAck.Role
	}

	var currentRole *pb.RoleInfo
	switch *flow {
	case "create":
		currentRole = enterGame(createRole().RoleId)
	case "enter":
		currentRole = enterGame(*roleID)
	default: // 空
		if len(ack.Roles) == 0 {
			currentRole = enterGame(createRole().RoleId)
		} else {
			id := *roleID
			if id == 0 {
				id = ack.Roles[0].RoleId
			}
			currentRole = enterGame(id)
		}
	}

	// 3.55 排行榜查询（进入游戏后）
	if *rankOp != "" {
		runRank(conn, *rankOp, *rankBoard, *rankOffset, *rankLimit, currentRole, waitFor, exit)
	}

	// 3.6 公会操作（进入游戏后）
	if *guildOp != "" {
		runGuild(conn, guildOpts{
			op: *guildOp, name: *guildName, id: *guildID,
			target: *targetRole, targetName: *targetName, notice: *notice,
			joinMode: *joinMode, position: *position, accept: *accept,
		}, waitFor, exit)
	}

	// 3.6 Hello 全链路：client → gate → game → gate → client
	if *hello {
		conn.Send(&pb.HelloReq{Content: *content})
		rawHl, ok := waitFor("helloack", 3*time.Second)
		if !ok {
			log.Errorf("hello ack timeout")
			exit(1)
		}
		hlAck := rawHl.(*pb.HelloAck)
		if hlAck.Content != *content {
			log.Errorf("hello content mismatch: got %q want %q", hlAck.Content, *content)
			exit(1)
		}
		log.Infof("hello ok content=%q", hlAck.Content)
	}

	// 4. 广播
	if *broadcast {
		var uids []int64
		for _, s := range strings.Split(*uidsFlag, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			v, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				log.Errorf("bad uids %q: %v", *uidsFlag, err)
				exit(1)
			}
			uids = append(uids, v)
		}
		conn.Send(&pb.BroadcastReq{Uids: uids, Content: *content})
		rawBc, ok := waitFor("broadcastack", 3*time.Second)
		if !ok {
			log.Errorf("broadcast ack timeout")
			exit(1)
		}
		bcAck := rawBc.(*pb.BroadcastAck)
		log.Infof("BroadcastAck count=%d", bcAck.Count)
	}

	// 5. 退出
	if *logout {
		conn.Send(&pb.LogoutReq{})
		rawLg, ok := waitFor("logoutack", 3*time.Second)
		if !ok {
			log.Errorf("logout ack timeout")
			exit(1)
		}
		lgAck := rawLg.(*pb.LogoutAck)
		if lgAck.Code != 0 {
			log.Errorf("logout failed code=%d reason=%q", lgAck.Code, lgAck.Reason)
			exit(1)
		}
		log.Infof("logout ok")
	}

	// 6. 停留 wait 时长：观察 NotifyNtf / KickNtf；断开则提前结束
	if *wait > 0 {
		timer := time.After(*wait)
	loop:
		for {
			select {
			case e := <-events:
				if e.kind == "closed" || e.kind == "connecterr" {
					break loop
				}
				// notify/kick 已由 handler 打日志
			case <-timer:
				break loop
			case <-done:
				break loop
			}
		}
	}

	exit(0)
}

// connectorSession 取默认会话
func connectorSession(p cellnet.Peer) cellnet.Session {
	return p.(cellnet.WSConnector).Session()
}

// signJWT 本地签发 HS256 测试 token
func signJWT(secret string, uid int64, account string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"uid":     uid,
		"account": account,
		"iat":     now.Unix(),
		"exp":     now.Add(time.Hour).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}
