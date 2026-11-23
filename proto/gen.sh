#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

# 依赖: protoc, protoc-gen-gogofaster, protoc-gen-msg
# go install github.com/gogo/protobuf/protoc-gen-gogofaster@v1.3.2
# go install github.com/davyxu/cellnet/protoc-gen-msg@v4.1.0
#
# 注意: protoc-gen-msg 要求 msg_out 参数为输出文件名（插件用 parameter 当文件名）

PATH="$(go env GOPATH)/bin:$PATH"

for f in gate.proto inner.proto; do
  base="${f%.proto}"
  protoc --gogofaster_out=paths=source_relative:. --proto_path=. "$f"
  protoc --msg_out="${base}.msgid.go:." --proto_path=. "$f"
done

gofmt -w ./*.go
