FROM golang:1.24-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /llm-proxy .

FROM alpine:3.19

LABEL org.opencontainers.image.source=https://github.com/prime-radiant-inc/llm-proxy
LABEL org.opencontainers.image.description="Transparent logging proxy for LLM API traffic"

RUN apk add --no-cache wget

COPY --from=builder /llm-proxy /usr/local/bin/llm-proxy

EXPOSE 9999

ENTRYPOINT ["/usr/local/bin/llm-proxy"]
CMD ["--service", "--port", "9999"]
