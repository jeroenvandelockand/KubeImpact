FROM node:24-alpine AS web-builder

WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS go-builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY rules/ ./rules/
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/kubeimpact ./cmd/api

FROM alpine:3.21

RUN apk add --no-cache ca-certificates \
    && addgroup -S kubeimpact \
    && adduser -S -D -H -G kubeimpact -u 10001 kubeimpact

COPY --from=go-builder /out/kubeimpact /usr/local/bin/kubeimpact
COPY --from=web-builder /web/dist /web

ENV GIN_MODE=release \
    KUBEIMPACT_ADDR=:8080 \
    KUBEIMPACT_WEB_DIR=/web

USER 10001:10001
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/api/v1/health || exit 1

ENTRYPOINT ["/usr/local/bin/kubeimpact"]
