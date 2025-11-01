# 使用多阶段构建
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o broker ./cmd/broker

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/broker .
EXPOSE 8080
CMD ["./broker"]