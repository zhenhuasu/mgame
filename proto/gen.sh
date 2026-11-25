#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

# 依赖: protoc, protoc-gen-gogofaster, protoc-gen-msg
# go install github.com/gogo/protobuf/protoc-gen-gogofaster@v1.3.2
# go install github.com/davyxu/cellnet/protoc-gen-msg@v4.1.0
#
# 注意:
# - protoc-gen-msg 要求 msg_out 参数为输出文件名（插件用 parameter 当文件名）
# - protoc-gen-msg 会为请求内所有 .proto 生成注册，inner.proto import gate.proto，
#   故只用 inner.proto 一次生成 msgid.go（含两份消息）；按 proto 分别生成会重复注册而 panic

PATH="$(go env GOPATH)/bin:$PATH"

for f in gate.proto inner.proto; do
  protoc --gogofaster_out=paths=source_relative:. --proto_path=. "$f"
done
protoc --msg_out="msgid.go:." --proto_path=. inner.proto

gofmt -w ./*.go
