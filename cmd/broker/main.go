package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"pigeon/internal/broker"
)

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}

func main() {
	// 创建一个根context，并设置取消函数
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // 确保在main函数返回前取消context

	// coordinator-service exposes port 80 (nodePort 30081) and maps to container 8081.
	// Use the service port (80) or omit it so in-cluster DNS resolves to port 80 by default.
	coordinatorURL := "http://coordinator-service/api/v1/assign"
	BrokerID := fetchBrokerID(coordinatorURL)
	if BrokerID == "" {
		log.Printf("Warning: failed to obtain coordinator ID from %s, continuing without it", coordinatorURL)
	} else {
		log.Printf("Obtained coordinator ID: %s", BrokerID)
	}

	srv := broker.NewServer("redis-service:6379", BrokerID)

	if BrokerID != "" {
		// 启动订阅以 BrokerID 为 topic 的 Redis 消息
		go srv.SubscribeTopic(ctx, BrokerID)
		// 启动心跳，每 4 秒发送一次到 coordinator
		go startHeartbeat(ctx, "http://coordinator-service/api/v1/brokers/heartbeat", BrokerID)
	}

	http.HandleFunc("/", srv.Handler)

	server := &http.Server{
		Addr:         ":8080",
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	log.Printf("Broker Server starting on :8080")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed to start: %v", err)
	}
}

// fetchBrokerID calls the coordinator assign endpoint and returns the id.
// It tries up to 3 times with a short delay.
func fetchBrokerID(url string) string {
	client := &http.Client{Timeout: 2 * time.Second}
	var lastErr error
	for i := 0; i < 3; i++ {
		resp, err := client.Get(url)
		if err != nil {
			lastErr = err
			time.Sleep(500 * time.Millisecond)
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			time.Sleep(500 * time.Millisecond)
			continue
		}

		var m map[string]string
		if err := json.Unmarshal(body, &m); err != nil {
			lastErr = err
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if id, ok := m["id"]; ok && id != "" {
			return id
		}
		time.Sleep(500 * time.Millisecond)
	}
	log.Printf("fetchBrokerID failed after retries: %v", lastErr)
	return ""
}

// startHeartbeat sends a heartbeat to coordinator every 4 seconds to refresh liveness.
// baseURL should be coordinator heartbeat endpoint (e.g. http://coordinator-service/api/v1/brokers/heartbeat)
func startHeartbeat(ctx context.Context, baseURL, brokerID string) {
	client := &http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("heartbeat: stopping for broker %s", brokerID)
			return
		case <-ticker.C:
			url := baseURL + "?id=" + brokerID
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
			if err != nil {
				log.Printf("heartbeat: create request error: %v", err)
				continue
			}
			resp, err := client.Do(req)
			if err != nil {
				log.Printf("heartbeat: send error: %v", err)
				continue
			}
			// drain and close
			_, _ = io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				log.Printf("heartbeat: unexpected status: %s", resp.Status)
			}
		}
	}
}
