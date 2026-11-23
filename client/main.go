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
	kind string // connected/loginack/logoutack/broadcastack/notify/kick/closed/pingack/connecterr
	raw  interface{}
}

func main() {
	addr := flag.String("addr", "ws://127.0.0.1:8801/ws", "ws:// 或 wss:// 地址")
	account := flag.String("account", "", "账号（空=仅 Ping 测试）")
	uid := flag.Int64("uid", 0, "uid（>0 触发登录流程）")
	token := flag.String("token", "", "token")
	jwtSecret := flag.String("jwt-secret", "", "非空则本地签发 HS256 token（覆盖 -token）")
	broadcast := flag.Bool("broadcast", false, "登录后触发一次广播")
	uidsFlag := flag.String("uids", "", "广播 uid 列表，逗号分隔；空=全局")
	content := flag.String("content", "hello", "广播内容")
	logout := flag.Bool("logout", false, "广播后发送退出")
	hello := flag.Bool("hello", false, "登录后发送 HelloReq（经 gate→game 全链路）")
	wait := flag.Duration("wait", 2*time.Second, "登录/广播后等待通知的时长")
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
	conn.Send(&pb.LoginReq{Account: *account, Uid: *uid, Token: loginToken})
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
	log.Infof("login ok uid=%d", ack.Uid)

	// 3.5 Hello 全链路：client → gate → game → gate → client
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
