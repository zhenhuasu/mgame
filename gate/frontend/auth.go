// Package frontend 与客户端通信侧：ws 装配、客户端消息处理、token 鉴权。
package frontend

import (
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"

	"github.com/zhenhuasu/mgame/config"
)

// TokenVerifier token 校验：返回权威 uid 与 account。
// 客户端传入的 account/uid 仅 dev 模式使用，jwt 模式以 token claims 为准。
type TokenVerifier func(account string, uid int64, token string) (int64, string, error)

// VerifyToken 由 ConfigureAuth 按配置注入，默认 dev 占位校验
var VerifyToken TokenVerifier = devVerifier

// debugHandlers 是否注册联调后门 handler（仅 dev 模式）
var debugHandlers = true

func devVerifier(account string, uid int64, token string) (int64, string, error) {
	if account == "" {
		return 0, "", errors.New("account required")
	}
	if uid <= 0 {
		return 0, "", errors.New("uid required")
	}
	if token == "" {
		return 0, "", errors.New("token required")
	}
	return uid, account, nil
}

type jwtClaims struct {
	UID     int64  `json:"uid"`
	Account string `json:"account"`
	jwt.RegisteredClaims
}

func newJWTVerifier(secret string) TokenVerifier {
	key := []byte(secret)
	return func(account string, uid int64, token string) (int64, string, error) {
		if token == "" {
			return 0, "", errors.New("token required")
		}
		claims := &jwtClaims{}
		_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
			return key, nil
		}, jwt.WithValidMethods([]string{"HS256"}))
		if err != nil {
			return 0, "", fmt.Errorf("invalid token: %w", err)
		}
		if claims.UID <= 0 {
			return 0, "", errors.New("token missing uid")
		}
		acct := claims.Account
		if acct == "" {
			acct = account
		}
		return claims.UID, acct, nil
	}
}

// Configure 按 auth 配置注入 token 校验器与联调后门开关。
// 须在前端启动前调用；会话 TTL 由 session.New 创建时注入。
func Configure(cfg config.Auth) {
	switch cfg.Mode {
	case "jwt":
		VerifyToken = newJWTVerifier(cfg.JWTSecret)
		debugHandlers = false
	default:
		VerifyToken = devVerifier
		debugHandlers = true
	}
}
