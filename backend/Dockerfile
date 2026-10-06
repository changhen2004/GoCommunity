FROM golang:1.25.6-alpine

WORKDIR /app

# 使用 go env -w 直接写入 Go 配置文件，比 ENV 更可靠
# goproxy.cn 在国内可访问，direct 做 fallback
RUN go env -w GOPROXY=https://goproxy.cn,direct && \
    go env -w GOSUMDB=sum.golang.google.cn

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# 预热 GOCACHE：运行时 go run 直接命中镜像里的编译缓存，冷启动 17s -> <1s
RUN go build ./...

EXPOSE 3000

CMD ["go", "run", "."]
