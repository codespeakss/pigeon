package broker

import (
	"context"
	"encoding/json"
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

// SubscribeTopic subscribes to the broker's Redis channel (topic). When a
// message arrives it calls deliver(clientID, msgBytes) so the caller (usually
// the main package) can forward the payload to the appropriate websocket
// client.
func (s *Server) SubscribeTopic(ctx context.Context, topic string, deliver func(clientID string, msgBytes []byte)) {
	rdb := redisClient.NewClient(s.redisAddr)

	go func() {
		err := redisClient.Subscribe(ctx, rdb, topic, func(msg string) {
			fmt.Printf("[Broker %s] Received message: %s\n", topic, msg)

			// Expect message to be JSON: {"type":"notification","payload":{...}}
			// Parse to extract client_id from payload if present.
			var envelope struct {
				Type    string          `json:"type"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal([]byte(msg), &envelope); err != nil {
				log.Printf("failed to unmarshal pubsub message: %v", err)
				return
			}

			// Try extract client_id from payload (payload could be an object)
			var payloadMap map[string]interface{}
			if err := json.Unmarshal(envelope.Payload, &payloadMap); err != nil {
				log.Printf("failed to unmarshal payload: %v", err)
				return
			}

			// client_id may be present as "client_id" or "target"; prefer client_id
			var clientID string
			if v, ok := payloadMap["client_id"]; ok {
				if s, ok := v.(string); ok {
					clientID = s
				}
			} else if v, ok := payloadMap["target"]; ok {
				if s, ok := v.(string); ok {
					clientID = s
				}
			}

			if clientID == "" {
				log.Printf("no client_id in payload, dropping message: %s", msg)
				return
			}

			// Deliver raw message bytes to the hub (caller will decide how to handle)
			if deliver != nil {
				deliver(clientID, []byte(msg))
			}
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
