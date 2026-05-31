# ── Stage 1: Build ────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

WORKDIR /app

# 의존성 먼저 캐시
COPY go.mod go.sum ./
RUN go mod download

# 소스 복사 및 빌드
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s" -o server ./cmd/main.go

# ── Stage 2: Runtime ───────────────────────────────────────────────────────────
FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata
ENV TZ=Asia/Seoul

WORKDIR /app

# 바이너리 복사
COPY --from=builder /app/server .

# 시드 데이터 복사 (볼륨이 비어있을 때 초기화에 사용)
COPY data/ ./data-seed/

# entrypoint 스크립트
COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh

# 런타임 설정
# DATA_DIR: Railway 볼륨 마운트 경로
# PORT:     Railway가 자동으로 주입
ENV PORT=8090
ENV DATA_DIR=/data

EXPOSE 8090

ENTRYPOINT ["/docker-entrypoint.sh"]
CMD ["./server"]
