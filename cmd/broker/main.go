package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	redis "github.com/redis/go-redis/v9"

	"pigeon/internal/broker"
	messages "pigeon/pkg/messages"
	rpkg "pigeon/pkg/redis"
)

const (
	// 允许等待的写入时间
	writeWait = 10 * time.Second
	// 允许读取的 PONG 消息的超时时间 (必须大于 pingPeriod)
	pongWait = 5 * time.Second
	// 发送 PING 消息的周期 (必须小于 pongWait)
	pingPeriod = (pongWait * 9) / 10
	// 允许的最大消息大小
	maxMessageSize = 512
	// TTL for client metadata stored in Redis. If a client stops heartbeating
	// its key will expire and no longer be stored.
	clientTTL = 30 * time.Second
)

// upgrader 用于将 HTTP 连接升级为 WebSocket 连接
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// 解决跨域问题
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// Message 定义了 WebSocket 消息的结构
type Message struct {
	Type    string          `json:"type"`              // 消息类型: notification, ack
	Payload json.RawMessage `json:"payload,omitempty"` // 消息内容
}

// AckPayload ACK 的载荷
type AckPayload struct {
	MsgID string `json:"msg_id"` // 对应的消息 ID
}

// ConnectedPayload 服务端在连接建立成功后发送给客户端的载荷，包含分配的 client_id
// ConnectedPayload moved to pkg/messages

// Client 是一个 WebSocket 客户端的中间表示
type Client struct {
	hub *Hub
	// WebSocket 连接
	conn *websocket.Conn
	// 带缓冲的 channel，用于出站消息
	send chan []byte
	// 客户端的唯一标识 (例如 ClientID)
	clientID string
}

// readPump 从 WebSocket 连接中异步读取消息
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		// update last active timestamp in redis
		go c.hub.TouchClient(c.clientID)
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[ERROR] readPump: %v", err)
			}
			log.Printf("[INFO] Client %s disconnected.", c.clientID)
			break
		}

		// update last active timestamp in redis on any received message
		go c.hub.TouchClient(c.clientID)

		var msg Message
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Printf("[WARN] Failed to unmarshal message from %s: %v", c.clientID, err)
			continue
		}

		switch msg.Type {
		case "ack":
			var payload AckPayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				log.Printf("[WARN] Failed to unmarshal ACK payload from %s: %v", c.clientID, err)
			} else {
				log.Printf("[INFO] Received ACK for MsgID: %s from Client: %s", payload.MsgID, c.clientID)
			}
		default:
			log.Printf("[INFO] Received unhandled message type '%s' from %s", msg.Type, c.clientID)
		}
	}
}

// writePump 将消息从 Hub 异步写入 WebSocket 连接
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("[ERROR] writePump ping: %v", err)
				return
			}
		}
	}
}

// Hub 维护所有活跃的客户端，并在其自己的 goroutine 中运行
type Hub struct {
	clients    map[string]*Client
	clientsMu  sync.RWMutex
	register   chan *Client
	unregister chan *Client
	// redis client for persisting client info
	redisClient *redis.Client
	// broker id where this hub is running
	brokerID string
}

func newHub(rdb *redis.Client, brokerID string) *Hub {
	return &Hub{
		clients:     make(map[string]*Client),
		register:    make(chan *Client),
		unregister:  make(chan *Client),
		redisClient: rdb,
		brokerID:    brokerID,
	}
}

// run 在一个单独的 goroutine 中运行 Hub
func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.clientsMu.Lock()
			h.clients[client.clientID] = client
			h.clientsMu.Unlock()
			log.Printf("[INFO] Client %s registered.", client.clientID)
			// persist registration info to redis
			go h.persistClientRegister(client.clientID)

		case client := <-h.unregister:
			h.clientsMu.Lock()
			if _, ok := h.clients[client.clientID]; ok {
				delete(h.clients, client.clientID)
				close(client.send)
				log.Printf("[INFO] Client %s unregistered.", client.clientID)
				// update last_active_at on unregister
				go h.TouchClient(client.clientID)
			}
			h.clientsMu.Unlock()
		}
	}
}

// persistClientRegister writes initial client info into redis
func (h *Hub) persistClientRegister(clientID string) {
	if h.redisClient == nil {
		return
	}
	ctx := context.Background()
	now := time.Now().Format(time.RFC3339)
	key := "client:" + clientID
	// store as a hash: id, registered_at, last_active_at, broker
	if err := h.redisClient.HSet(ctx, key, "id", clientID, "registered_at", now, "last_active_at", now, "broker", h.brokerID).Err(); err != nil {
		log.Printf("[ERROR] persistClientRegister HSet: %v", err)
		return
	}
	// set a TTL so stale clients are removed if they stop heartbeating
	if err := h.redisClient.Expire(ctx, key, clientTTL).Err(); err != nil {
		log.Printf("[ERROR] persistClientRegister Expire: %v", err)
	}
}

// TouchClient updates the last_active_at timestamp for a client in redis
func (h *Hub) TouchClient(clientID string) {
	if h.redisClient == nil {
		return
	}
	ctx := context.Background()
	now := time.Now().Format(time.RFC3339)
	key := "client:" + clientID
	if err := h.redisClient.HSet(ctx, key, "last_active_at", now).Err(); err != nil {
		log.Printf("[ERROR] TouchClient HSet: %v", err)
		return
	}
	// refresh TTL so active clients stay stored
	if err := h.redisClient.Expire(ctx, key, clientTTL).Err(); err != nil {
		log.Printf("[ERROR] TouchClient Expire: %v", err)
	}
}

// sendToClient 向指定 ClientID 的客户端发送消息
func (h *Hub) sendToClient(clientID string, message []byte) {
	h.clientsMu.RLock()
	client, ok := h.clients[clientID]
	h.clientsMu.RUnlock()

	if ok {
		select {
		case client.send <- message:
		default:
			log.Printf("[WARN] Client %s send channel full. Closing connection.", clientID)
			h.unregister <- client
		}
	} else {
		log.Printf("[WARN] Client %s not found. Message dropped.", clientID)
	}
}

// validateToken 是一个 Token 验证的存根 (Stub)
func validateToken(tokenStr string) (string, bool) {
	parts := strings.Split(tokenStr, " ")
	if len(parts) != 2 || parts[0] != "Bearer" {
		return "", false
	}
	token := parts[1]

	if token == "" {
		return "", false
	}

	const masterToken = "test-token"
	if token == masterToken {
		t := time.Now().Format("03:04:05PM") // 时间部分
		r := rand.Intn(1000000)              // 生成一个 0~999999 的随机数
		return fmt.Sprintf("Client-%s-%06d", t, r), true
	}

	if token == "user-A-token" {
		return "Client-A", true
	}
	if token == "user-B-token" {
		return "Client-B", true
	}

	return "", false
}

// serveWs 处理来自 HTTP 请求的 WebSocket 升级
func serveWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	clientID, ok := validateToken(authHeader)
	if !ok {
		http.Error(w, "Auth Fail", http.StatusUnauthorized)
		log.Printf("[WARN] auth connection attempt. Token: %s", authHeader)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ERROR] Failed to upgrade connection: %v", err)
		return
	}

	client := &Client{
		hub:      hub,
		conn:     conn,
		send:     make(chan []byte, 256),
		clientID: clientID,
	}

	client.hub.register <- client

	// Start writePump first so we can push an initial "connected" message
	go client.writePump()

	// Send connected message to inform client of its assigned client_id
	connectedPayload := messages.ConnectedPayload{ClientID: clientID}
	payloadBytes, _ := json.Marshal(connectedPayload)
	connMsg := Message{Type: "connected", Payload: payloadBytes}
	connMsgBytes, _ := json.Marshal(connMsg)
	// non-blocking send: if channel is full, drop the message (connection likely closing)
	select {
	case client.send <- connMsgBytes:
	default:
		log.Printf("[WARN] client %s send channel full when sending connected message", clientID)
	}

	go client.readPump()
}

// 模拟的后台任务，用于发送异步通知
func startDemoNotificationSender(hub *Hub) {
	time.Sleep(5 * time.Second)

	log.Println("--- [DEMO] Sending notification to Client-A ---")

	payload := messages.NotificationPayload{
		MsgID: "msg-12345",
		Data:  "This is an asynchronous notification!",
	}
	payloadBytes, _ := json.Marshal(payload)
	msg := Message{
		Type:    "notification",
		Payload: payloadBytes,
	}
	msgBytes, _ := json.Marshal(msg)

	hub.sendToClient("Client-A", msgBytes)
}

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	rand.Seed(time.Now().UnixNano())
}

func main() {
	// 创建一个根context，并设置取消函数
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // 确保在main函数返回前取消context

	coordinatorURL := "http://coordinator-service/api/v1/assign"
	BrokerID := fetchBrokerID(coordinatorURL)
	if BrokerID == "" {
		log.Printf("Warning: failed to obtain coordinator ID from %s, continuing without it", coordinatorURL)
	} else {
		log.Printf("Obtained coordinator ID: %s", BrokerID)
	}

	srv := broker.NewServer("redis-service:6379", BrokerID)

	// WebSocket hub — create Redis client and hub early so SubscribeTopic can
	// deliver messages into the hub.
	rdb := rpkg.NewClient("redis-service:6379")
	hub := newHub(rdb, BrokerID)
	go hub.run()

	if BrokerID != "" {
		// 启动订阅以 BrokerID 为 topic 的 Redis 消息，并将收到的消息分发到 hub
		go srv.SubscribeTopic(ctx, BrokerID, func(clientID string, msgBytes []byte) {
			// msgBytes is the raw JSON envelope; broker hub expects the full
			// message bytes (same shape as notifications used elsewhere).
			go hub.sendToClient(clientID, msgBytes)
		})
		// 启动心跳，每 4 秒发送一次到 coordinator
		go startHeartbeat(ctx, "http://coordinator-service/api/v1/brokers/heartbeat", BrokerID)
	}

	// HTTP routes
	http.HandleFunc("/", srv.Handler)
	http.HandleFunc("/api/v1/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(hub, w, r)
	})

	http.HandleFunc("/api/v1/broadcast", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Targets []string `json:"targets,omitempty"`
			Data    string   `json:"data"`
			MsgID   string   `json:"msg_id,omitempty"`
		}

		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		msgID := req.MsgID
		if msgID == "" {
			msgID = "msg-" + time.Now().Format("20060102T150405.000")
		}

		payload := messages.NotificationPayload{
			MsgID: msgID,
			Data:  req.Data,
		}
		payloadBytes, _ := json.Marshal(payload)
		msg := Message{
			Type:    "notification",
			Payload: payloadBytes,
		}
		msgBytes, _ := json.Marshal(msg)

		if len(req.Targets) == 0 {
			hub.clientsMu.RLock()
			for uid := range hub.clients {
				go hub.sendToClient(uid, msgBytes)
			}
			hub.clientsMu.RUnlock()
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte("broadcasted to all"))
			return
		}

		for _, t := range req.Targets {
			go hub.sendToClient(t, msgBytes)
		}
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte("broadcasted to targets"))
	})

	// 提供 Hub clients 信息的 HTTP 接口
	http.HandleFunc("/api/v1/hub/clients", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// 读取 clients 列表（线程安全）
		hub.clientsMu.RLock()
		clients := make([]string, 0, len(hub.clients))
		for id := range hub.clients {
			clients = append(clients, id)
		}
		count := len(clients)
		hub.clientsMu.RUnlock()

		resp := map[string]interface{}{
			"count":   count,
			"clients": clients,
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	})

	go startDemoNotificationSender(hub)

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

// startHeartbeat sends a heartbeat to coordinator every 4 seconds to refresh liveness.
// baseURL should be coordinator heartbeat endpoint (e.g. http://coordinator-service/api/v1/brokers/heartbeat)
func startHeartbeat(ctx context.Context, baseURL, brokerID string) {
	client := &http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("heartbeat: stopping for broker %s", brokerID)
			return
		case <-ticker.C:
			url := baseURL + "?id=" + brokerID
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
			if err != nil {
				log.Printf("heartbeat: create request error: %v", err)
				continue
			}
			resp, err := client.Do(req)
			if err != nil {
				log.Printf("heartbeat: send error: %v", err)
				continue
			}
			// drain and close
			_, _ = io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				log.Printf("heartbeat: unexpected status: %s", resp.Status)
			}
		}
	}
}
