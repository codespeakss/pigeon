package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"time"

	"github.com/gorilla/websocket"
)

// Message 定义（与服务端保持一致）
type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}
type NotificationPayload struct {
	MsgID string `json:"msg_id"`
	Data  string `json:"data"`
}
type AckPayload struct {
	MsgID string `json:"msg_id"`
}

// 服务端设置的超时时间（客户端也需要）
const (
	pongWait   = 5 * time.Second
	pingPeriod = (pongWait * 9) / 10
	writeWait  = 10 * time.Second
)

// readPump 负责读取服务端消息，并在收到 "notification" 时发送 ACK
func readPump(conn *websocket.Conn, sendChan chan<- []byte, done chan<- struct{}) {
	defer func() {
		close(done) // 通知 main goroutine 连接已断开
	}()
	conn.SetReadLimit(512)
	// 客户端也需要设置 PONG 处理器来维持连接
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[ERROR] readPump: %v", err)
			}
			log.Printf("[INFO] Connection closed by server.")
			break
		}

		var msg Message
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Printf("[WARN] Failed to unmarshal message: %v", err)
			continue
		}

		switch msg.Type {
		case "notification":
			var payload NotificationPayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				log.Printf("[WARN] Failed to unmarshal notification payload: %v", err)
				continue
			}

			log.Printf("[RECV] Notification! MsgID: %s, Data: %s", payload.MsgID, payload.Data)

			// ** 收到通知，立即发送 ACK **
			ackPayload := AckPayload{MsgID: payload.MsgID}
			payloadBytes, _ := json.Marshal(ackPayload)
			ackMsg := Message{Type: "ack", Payload: payloadBytes}
			ackMsgBytes, _ := json.Marshal(ackMsg)

			// 将 ACK 消息发送到 writePump 的 channel
			sendChan <- ackMsgBytes
			log.Printf("[SEND] ACK for MsgID: %s", payload.MsgID)

		default:
			log.Printf("[RECV] Unhandled message type: %s", msg.Type)
		}
	}
}

// writePump 负责发送心跳和 ACK 消息
func writePump(conn *websocket.Conn, sendChan <-chan []byte, interrupt <-chan os.Signal) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		conn.Close()
	}()

	for {
		select {
		case message, ok := <-sendChan:
			// 从 readPump 收到 ACK 消息
			conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
				log.Println("[ERROR] writePump send: ", err)
				return
			}

		case <-ticker.C:
			// 客户端也主动发送 Ping 以保持连接（双向心跳）
			conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Println("[ERROR] writePump ping: ", err)
				return
			}

		case <-interrupt:
			// 收到中断信号 (Ctrl+C)
			log.Println("Interrupt received, closing connection...")
			// 发送 CloseMessage
			err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				log.Println("[ERROR] write close:", err)
			}
			// 等待服务器关闭连接（或超时）
			select {
			case <-time.After(time.Second):
			}
			return
		}
	}
}

func main() {
	// 允许通过命令行参数传入 Token（可选）。如果未提供，则随机生成一个用于测试。
	token := flag.String("token", "", "Auth token for WebSocket connection (optional). If empty, a random token will be generated for this run")
	addr := flag.String("addr", "localhost:8080", "WebSocket server address")
	flag.Parse()

	log.SetFlags(log.Ltime)

	// 捕获 Ctrl+C 中断信号
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// 准备连接 URL
	u := url.URL{Scheme: "ws", Host: *addr, Path: "/ws"}
	log.Printf("Connecting to %s", u.String())

	// ** 关键：在 Header 中设置 Token **
	headers := http.Header{}
	// 如果未传入 token，则生成一个随机 token（16 字节 -> 32 hex chars）
	tokenVal := *token
	if tokenVal == "" {
		tokenVal = "test-token"
	}
	headers.Add("Authorization", "Bearer "+tokenVal)

	// 拨号连接
	conn, resp, err := websocket.DefaultDialer.Dial(u.String(), headers)
	if err != nil {
		if resp != nil {
			log.Fatalf("Failed to connect: %v (Status: %s)", err, resp.Status)
		}
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	log.Println("Connection established. Waiting for messages...")

	done := make(chan struct{})      // 用于 readPump 通知 main 退出
	sendChan := make(chan []byte, 1) // 用于 readPump 向 writePump 发送 ACK

	// 启动读写 goroutine
	go readPump(conn, sendChan, done)
	go writePump(conn, sendChan, interrupt)

	// 等待连接断开（readPump 退出）或用户中断
	<-done
	log.Println("Client shutting down.")
}
