package broker

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	redisClient "pigeon/pkg/redis"
)

type Server struct {
	redisAddr string
	BrokerID  string
}

func NewServer(redisAddr string, BrokerID string) *Server {
	return &Server{
		redisAddr: redisAddr,
		BrokerID:  BrokerID,
	}
}

func (s *Server) SubscribeTopic(ctx context.Context, topic string) {
	rdb := redisClient.NewClient(s.redisAddr)
	// topic := s.BrokerID
	go func() {
		err := redisClient.Subscribe(ctx, rdb, topic, func(msg string) {
			fmt.Printf("[Broker %s] Received message: %s\n", topic, msg)
		})
		if err != nil {
			log.Printf("Subscribe error: %v", err)
		}
	}()
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

	// 返回当前连接的 websocket 客户端列表
	if _, err := fmt.Fprintf(w, "23:03 Host: %s, BrokerID: %s, Redis Count: %d ", hostname, s.BrokerID, count); err != nil {
		log.Printf("Error writing response: %v", err)
	}
}
