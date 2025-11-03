# 使用多阶段构建
FROM golang:1.22-alpine@sha256:1699c10032ca2582ec89a24a1312d986a3f094aed3d5c1147b19880afe40e052 AS builder
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o coordinator ./cmd/coordinator

FROM alpine:3.20@sha256:765942a4039992336de8dd5db680586e1a206607dd06170ff0a37267a9e01958
WORKDIR /app
COPY --from=builder /app/coordinator .
EXPOSE 8081
CMD ["./coordinator"]