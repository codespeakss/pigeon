package main

import (
	"log"
	"net/http"
	"time"

	"github.com/codespeakss/k8s/internal/coordinator"
)

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}

func main() {
	srv := coordinator.NewServer("redis-service:6379")
	http.HandleFunc("/", srv.Handler)
	http.HandleFunc("/assign", srv.AssignHandler)
	http.HandleFunc("/broker", srv.GetBrokerHandler)
	http.HandleFunc("/brokers", srv.GetAllBrokersHandler)

	server := &http.Server{
		Addr:         ":8081", // 注意这里使用不同的端口
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	log.Printf("Coordinator Server starting on :8081")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed to start: %v", err)
	}
}
