package main

import (
	"encoding/json"
	"flag"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"time"

	"pigeon/pkg/messages"

	"github.com/gorilla/websocket"
)

// Message 定义（与服务端保持一致）
type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type AckPayload struct {
	MsgID string `json:"msg_id"`
}

// ConnectedPayload moved to pkg/messages

// 服务端设置的超时时间（客户端也需要）
const (
	pongWait   = 5 * time.Second
	pingPeriod = (pongWait * 9) / 10
	writeWait  = 10 * time.Second
)

const (
	// baseBackoff is the initial backoff duration
	baseBackoff = 5000 * time.Millisecond
	// maxBackoff caps the exponential backoff
	maxBackoff = 60 * time.Second
)

// backoff returns a randomized exponential backoff duration (full jitter)
// attempt starts at 0 for the first retry. It will cap at maxBackoff.
func backoff(attempt int) time.Duration {
	// compute exponential backoff value with cap
	exp := baseBackoff
	for i := 0; i < attempt; i++ {
		exp = exp * 2
		if exp >= maxBackoff {
			exp = maxBackoff
			break
		}
	}

	// full jitter: random value in [0, exp]
	if exp <= 0 {
		return baseBackoff
	}
	jitter := time.Duration(rnd.Int63n(int64(exp)))
	return jitter
}

// rnd is a package-level PRNG to produce jitter. Initialized in main().
var rnd *rand.Rand

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
		case "connected":
			var payload messages.ConnectedPayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				log.Printf("[WARN] Failed to unmarshal connected payload: %v", err)
				continue
			}

			// 服务端在连接建立成功后告知客户端 client-id
			log.Printf("[INFO] Connected. Assigned client_id: %s", payload.ClientID)

			// 如果需要，后续可将 client-id 写入本地状态或发送到其他 goroutine
			continue

		case "notification":
			var payload messages.NotificationPayload
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
			log.Printf("             [SEND] ACK for MsgID: %s", payload.MsgID)

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
			time.Sleep(time.Second)
			return
		}
	}
}

func main() {
	// 允许通过命令行参数传入 Token（可选）。如果未提供，则随机生成一个用于测试。
	token := flag.String("token", "", "Auth token for WebSocket connection (optional). If empty, a random token will be generated for this run")
	addr := flag.String("addr", "localhost:30080", "WebSocket server address")
	flag.Parse()

	log.SetFlags(log.Ltime)

	// initialize package-level PRNG for jitter
	rnd = rand.New(rand.NewSource(time.Now().UnixNano()))

	// 捕获 Ctrl+C 中断信号
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// 准备连接 URL（在循环内部重建 URL/headers 以便重连时使用相同设置）
	u := url.URL{Scheme: "ws", Host: *addr, Path: "/api/v1/ws"}

	// 如果未传入 token，则生成一个默认 token（测试用）
	tokenVal := *token
	if tokenVal == "" {
		tokenVal = "test-token"
	}

	// 连接/重连循环：当连接异常断开时，释放资源并重试
	attempt := 0
	for {
		if attempt > -1 { // sure
			// 指数退避 + 随机抖动
			sleep := backoff(attempt)
			log.Printf("Trying in %v (attempt=%d)", sleep, attempt)
			time.Sleep(sleep)
		}

		log.Printf("Connecting to %s", u.String())

		// ** 关键：在 Header 中设置 Token **
		headers := http.Header{}
		headers.Add("Authorization", "Bearer "+tokenVal)

		// 拨号连接
		conn, resp, err := websocket.DefaultDialer.Dial(u.String(), headers)
		if err != nil {
			if resp != nil {
				log.Printf("Failed to connect: %v (Status: %s). Will retry...", err, resp.Status)
			} else {
				log.Printf("Failed to connect: %v. Will retry...", err)
			}

			// 在重连前检查是否收到退出信号（非阻塞）
			select {
			case <-interrupt:
				log.Println("Interrupt received while attempting to connect. Exiting.")
				return
			default:
			}

			attempt++
			continue
		}

		log.Println("Connection established. Waiting for messages...")
		// reset attempt counter on success
		attempt = 0

		// 用于 readPump 通知 main 退出（连接断开）
		done := make(chan struct{})
		// 用于 readPump 向 writePump 发送 ACK
		sendChan := make(chan []byte, 16)

		// 启动读写 goroutine
		go readPump(conn, sendChan, done)
		go writePump(conn, sendChan, interrupt)

		// 等待连接断开或用户中断
		select {
		case <-done:
			// readPump 退出 —— 连接已断开（可能是异常断开）
			log.Println("Connection closed. Cleaning up resources and will retry...")

			// 清理：关闭发送通道以通知 writePump 停止（安全关闭）
			func() {
				defer func() { recover() }()
				close(sendChan)
			}()

			// 关闭底层连接（若未自动关闭）
			conn.Close()

			// 指数退避 + 随机抖动后重试
			sleep := backoff(attempt)
			log.Printf("Connection lost. Trying in %v (attempt=%d)", sleep, attempt)
			time.Sleep(sleep)
			attempt++
			continue

		case <-interrupt:
			// 收到中断信号 (Ctrl+C)，让 writePump 发送 CloseMessage 并退出
			log.Println("Interrupt received, shutting down client...")
			// 关闭连接 politely: writePump listens to interrupt and will attempt to close
			// 等待短时间让 writePump 发送 CloseMessage
			time.Sleep(1 * time.Second)
			// 最终清理并退出
			conn.Close()
			return
		}
	}
}
