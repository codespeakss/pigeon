# 使用多阶段构建
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o coordinator ./cmd/coordinator

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/coordinator .
EXPOSE 8081
CMD ["./coordinator"]