package coordinator

import (
	"fmt"
	"log"
	"net/http"
	"os"
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

	// ctx := context.Background()
	// rdb := redisClient.NewClient(s.redisAddr)

	// 处理协调逻辑
	if _, err := fmt.Fprintf(w, "23:09 Coordinator Host: %s", hostname); err != nil {
		log.Printf("Error writing response: %v", err)
	}
}
