package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

}

func handler(w http.ResponseWriter, r *http.Request) {
	hostname, err := os.Hostname()
	if err != nil {
		http.Error(w, "Unable to get hostname", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{
		Addr:     "redis-service:6379",
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	// 模拟访问
	key := "page:visit_count"

	count, err := rdb.Incr(ctx, key).Result() // 每访问一次自增 1
	if err != nil {
		panic(err)
	}

	fmt.Printf("访问次数: %d\n", count)

	if _, err := fmt.Fprintf(w, "Host: %s, Redis Count: %d", hostname, count); err != nil {
		log.Printf("Error writing response: %v", err)
	}
}

func main() {
	http.HandleFunc("/", handler)

	server := &http.Server{
		Addr:         ":8080",
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	log.Printf("Server starting on :8080")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed to start: %v", err)
	}
}
