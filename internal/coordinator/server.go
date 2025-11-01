package coordinator

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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

// Handler is the default coordinator handler that returns basic info
func (s *Server) Handler(w http.ResponseWriter, r *http.Request) {
	hostname, err := os.Hostname()
	if err != nil {
		http.Error(w, "Unable to get hostname", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	// 处理协调逻辑
	if _, err := fmt.Fprintf(w, "23:09 Coordinator Host: %s", hostname); err != nil {
		log.Printf("Error writing response: %v", err)
	}
}

// AssignHandler generates and returns a random id assigned by the coordinator.
// Response JSON: {"id":"..."}
func (s *Server) AssignHandler(w http.ResponseWriter, r *http.Request) {
	// generate 16 random bytes and hex-encode them
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		http.Error(w, "failed to generate id", http.StatusInternalServerError)
		log.Printf("failed to generate id: %v", err)
		return
	}
	id := hex.EncodeToString(b)

	resp := map[string]string{"id": id}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("failed to write assign response: %v", err)
	}
}
