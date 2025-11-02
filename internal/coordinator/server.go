package coordinator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	rpkg "github.com/codespeakss/k8s/pkg/redis"
	"github.com/redis/go-redis/v9"
)

type Server struct {
	redisAddr   string
	redisClient *redis.Client
}

func NewServer(redisAddr string) *Server {
	client := rpkg.NewClient(redisAddr)
	return &Server{
		redisAddr:   redisAddr,
		redisClient: client,
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
	// generate 4 random bytes and hex-encode them
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		http.Error(w, "failed to generate id", http.StatusInternalServerError)
		log.Printf("failed to generate id: %v", err)
		return
	}
	id := "broker-" + hex.EncodeToString(b)

	// persist assignment to redis
	meta := map[string]string{
		"id":          id,
		"assigned_at": time.Now().UTC().Format(time.RFC3339),
		"remote_addr": r.RemoteAddr,
	}
	data, _ := json.Marshal(meta)
	ctx := context.Background()
	key := fmt.Sprintf("broker:%s", id)
	ttl := 10 * time.Second
	if err := s.redisClient.Set(ctx, key, data, ttl).Err(); err != nil {
		log.Printf("failed to persist broker assignment to redis: %v", err)
		http.Error(w, "failed to persist assignment", http.StatusInternalServerError)
		return
	}

	resp := map[string]string{"id": id}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("failed to write assign response: %v", err)
	}
}

// GetBrokerHandler looks up broker assignment information from redis by ?id=<broker-id>
func (s *Server) GetBrokerHandler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id query param", http.StatusBadRequest)
		return
	}
	key := fmt.Sprintf("broker:%s", id)
	ctx := context.Background()
	val, err := s.redisClient.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		log.Printf("redis get error: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// value is stored as JSON already
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(val)); err != nil {
		log.Printf("failed to write get response: %v", err)
	}
}

// GetAllBrokersHandler returns an array of all brokers
func (s *Server) GetAllBrokersHandler(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	keys, err := s.redisClient.Keys(ctx, "broker:*").Result()
	if err != nil {
		log.Printf("failed to retrieve broker keys: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	var brokers []map[string]string
	for _, key := range keys {
		val, err := s.redisClient.Get(ctx, key).Result()
		if err != nil {
			if err == redis.Nil {
				continue
			}
			log.Printf("redis get error: %v", err)
			continue
		}
		var broker map[string]string
		if err := json.Unmarshal([]byte(val), &broker); err != nil {
			log.Printf("failed to unmarshal broker data: %v", err)
			continue
		}
		brokers = append(brokers, broker)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(brokers); err != nil {
		log.Printf("failed to write brokers response: %v", err)
	}
}
