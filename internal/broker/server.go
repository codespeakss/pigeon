package broker

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	redisClient "github.com/codespeakss/k8s/pkg/redis"
)

type Server struct {
	redisAddr string
}

func NewServer(redisAddr string) *Server {
	return &Server{
		redisAddr: redisAddr,
	}
}

func (s *Server) Handler(w http.ResponseWriter, r *http.Request) {
	hostname, err := os.Hostname()
	if err != nil {
		http.Error(w, "Unable to get hostname", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	ctx := context.Background()
	rdb := redisClient.NewClient(s.redisAddr)

	// 模拟访问
	key := "page:visit_count"

	count, err := rdb.Incr(ctx, key).Result() // 每访问一次自增 1
	if err != nil {
		panic(err)
	}

	fmt.Printf("访问次数: %d\n", count)

	if _, err := fmt.Fprintf(w, "23:03 Host: %s, Redis Count: %d", hostname, count); err != nil {
		log.Printf("Error writing response: %v", err)
	}
}
