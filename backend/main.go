package main

import (
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/guobinqiu/vue2-go-websocket-protobuf-demo2/chat"
	"google.golang.org/protobuf/proto"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer ws.Close()

	// 设置消息最大长度
	ws.SetReadLimit(512)

	// 用原子变量存储最后一次收到pong时间戳
	var lastPongUnix int64 = time.Now().Unix()

	// 注释掉 尝试自己实现 pong 消息处理
	// 监听 pong 消息, 收到后更新 lastPongUnix
	// ws.SetPongHandler(func(string) error {
	// 	log.Println("收到客户端Pong")
	// 	atomic.StoreInt64(&lastPongUnix, time.Now().Unix())
	// 	return nil
	// })

	// 心跳检测
	go startHeartbeat(ws, &lastPongUnix)

	for {
		// 读取消息
		msgType, msgBytes, err := ws.ReadMessage()
		if err != nil {
			log.Printf("error: %v", err)
			break
		}

		// 若前端发送pong消息
		if msgType == websocket.TextMessage && string(msgBytes) == "pong" {
			log.Println("收到客户端pong")
			atomic.StoreInt64(&lastPongUnix, time.Now().Unix())
			continue
		}

		// 若前端发送二进制消息
		if msgType == websocket.BinaryMessage {
			chatMsg := &chat.ChatMessage{}
			if err := proto.Unmarshal(msgBytes, chatMsg); err != nil {
				log.Printf("Failed to unmarshal: %v", err)
				continue
			}

			fmt.Printf("Received message from %s: %s\n", chatMsg.User, chatMsg.Text)

			// echo back
			if buf, err := proto.Marshal(chatMsg); err == nil {

				// 若客户端断网或关闭，WriteMessage 会失败
				if err := ws.WriteMessage(websocket.BinaryMessage, buf); err != nil {

					// 断网之后连接就作废了需要重开新的连接
					// 连接失效后必须关闭，避免资源泄漏
					// ws.Close() 会触发客户端的 onclose 回调
					ws.Close()

					log.Printf("Write echo failed: %v", err)

					// 退出for循环
					break
				}
			}
		}
	}
}

func main() {
	http.HandleFunc("/ws", handleConnections)
	log.Println("Server started on :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}

func startHeartbeat(ws *websocket.Conn, lastPongUnix *int64) {
	pingInterval := 5 * time.Second // 每5秒发一次 Ping
	pongTimeout := 3 * time.Second  // 允许客户端最多3秒内回复 Pong

	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for range ticker.C {
		// 检查 pong 超时，处理未立即断开的“假死”连接（如休眠、断网）
		lastPong := time.Unix(atomic.LoadInt64(lastPongUnix), 0)
		if time.Since(lastPong) > pingInterval+pongTimeout { // 距离上次收到Pong已经超过了8秒就判定客户端断线
			log.Println("未收到客户端pong，断开连接")
			ws.Close()
			return
		}

		log.Println("服务端发送ping")

		// 检查 TCP 写入是否失败，客户端崩溃或连接异常可立即发现
		if err := ws.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
			// 断网之后连接就作废了需要重开新的连接
			// 连接失效后必须关闭，避免资源泄漏
			// ws.Close() 会触发客户端的 onclose 回调
			ws.Close()

			log.Println("服务端发送ping失败，断开连接:", err)

			// 退出这个协程
			return
		}
	}
}
