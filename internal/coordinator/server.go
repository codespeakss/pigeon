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

	rpkg "pigeon/pkg/redis"

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
func (s *Server) RootHandler(w http.ResponseWriter, r *http.Request) {
	hostname, err := os.Hostname()
	if err != nil {
		http.Error(w, "Unable to get hostname", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	// 处理协调逻辑
	if _, err := fmt.Fprintf(w, "17:00 Coordinator Host: %s", hostname); err != nil {
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

// HeartbeatHandler refreshes the broker's redis key TTL and updates last_seen timestamp.
// Expected query param: ?id=<broker-id>
func (s *Server) HeartbeatHandler(w http.ResponseWriter, r *http.Request) {
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
		log.Printf("redis get error in heartbeat: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	var meta map[string]string
	if err := json.Unmarshal([]byte(val), &meta); err != nil {
		log.Printf("failed to unmarshal broker data in heartbeat: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// update last_seen and refresh TTL
	meta["last_seen"] = time.Now().UTC().Format(time.RFC3339)
	data, _ := json.Marshal(meta)
	ttl := 10 * time.Second
	if err := s.redisClient.Set(ctx, key, data, ttl).Err(); err != nil {
		log.Printf("failed to persist heartbeat to redis: %v", err)
		http.Error(w, "failed to persist heartbeat", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "id": id})
}

// GetClientsHandler retrieves all clients managed by brokers
func (s *Server) GetClientsHandler(w http.ResponseWriter, r *http.Request) {
	// The broker persists each client as a separate hash keyed by "client:<id>".
	// Previously this handler tried to HGETALL "clients" which doesn't match
	// the way brokers write client data (they write keys like "client:Client-A").
	// To be robust we now scan for keys "client:*" and aggregate each hash.
	ctx := context.Background()

	keys, err := s.redisClient.Keys(ctx, "client:*").Result()
	if err != nil {
		log.Printf("failed to retrieve client keys: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	var clients []map[string]string
	for _, key := range keys {
		m, err := s.redisClient.HGetAll(ctx, key).Result()
		if err != nil {
			log.Printf("redis HGetAll error for %s: %v", key, err)
			continue
		}
		if len(m) == 0 {
			// skip empty results
			continue
		}
		clients = append(clients, m)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(clients); err != nil {
		log.Printf("failed to write clients response: %v", err)
	}
}

func (s *Server) SendToClientHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ClientID string `json:"client_id"`
		Data     string `json:"data"`
		MsgID    string `json:"msg_id,omitempty"`
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.ClientID == "" || req.Data == "" {
		http.Error(w, "missing client_id or data", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	// client info stored as hash key: client:<id> with field "broker"
	key := fmt.Sprintf("client:%s", req.ClientID)
	brokerID, err := s.redisClient.HGet(ctx, key, "broker").Result()
	if err != nil {
		if err == redis.Nil {
			http.Error(w, "client not found", http.StatusNotFound)
			return
		}
		log.Printf("redis HGet error: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Build the same message shape brokers expect for notifications.
	type NotificationPayload struct {
		MsgID    string `json:"msg_id"`
		Data     string `json:"data"`
		ClientID string `json:"client_id"`
	}
	type Message struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload,omitempty"`
	}

	msgID := req.MsgID
	if msgID == "" {
		msgID = "msg-" + time.Now().Format("20060102T150405.000")
	}

	payload := NotificationPayload{MsgID: msgID, Data: req.Data, ClientID: req.ClientID}
	payloadBytes, _ := json.Marshal(payload)
	msg := Message{Type: "notification", Payload: payloadBytes}
	msgBytes, _ := json.Marshal(msg)

	// Publish to the broker's channel (brokerID). Brokers subscribe to a
	// channel named after their BrokerID.
	topic := brokerID
	if err := s.redisClient.Publish(ctx, topic, string(msgBytes)).Err(); err != nil {
		log.Printf("redis publish error to %s: %v", topic, err)
		http.Error(w, "failed to publish", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "accepted", "broker": brokerID, "msg_id": msgID})
}
