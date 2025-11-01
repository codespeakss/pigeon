package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/codespeakss/k8s/internal/broker"
)

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}

func main() {
	// coordinator-service exposes port 80 (nodePort 30081) and maps to container 8081.
	// Use the service port (80) or omit it so in-cluster DNS resolves to port 80 by default.
	coordinatorURL := "http://coordinator-service/assign"
	BrokerID := fetchBrokerID(coordinatorURL)
	if BrokerID == "" {
		log.Printf("Warning: failed to obtain coordinator ID from %s, continuing without it", coordinatorURL)
	} else {
		log.Printf("Obtained coordinator ID: %s", BrokerID)
	}

	srv := broker.NewServer("redis-service:6379", BrokerID)
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
