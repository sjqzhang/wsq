package main

import (
	"fmt"
	"github.com/gorilla/websocket"
	"math/rand"
	"net/http"
	"sync"
	"time"
)



// DefaultDialer is a dialer with all fields set to the default values.
var DefaultDialer = &websocket.Dialer{
	Proxy:            http.ProxyFromEnvironment,
	HandshakeTimeout: 60*3 * time.Second,
}

func main() {
	url := "ws://127.0.0.1:8866/ws" // WebSocket服务器的URL
	numConnections := 40000         // 要创建的连接数量

	var wg sync.WaitGroup
	wg.Add(numConnections*10)

	for j := 0; j < 9; j++ {

		url = fmt.Sprintf("ws://192.168.0.%v:8866/ws", j)

		for i := 0; i < numConnections; i++ {
			go func(url string) {
				defer wg.Done()

				// random sleep
				time.Sleep(time.Duration(rand.Intn(1000*100)) * time.Millisecond)
				// 建立WebSocket连接
				conn, _, err := DefaultDialer.Dial(url, nil)
				if err != nil {
					fmt.Println("无法建立WebSocket连接:", err)
					return
				}
				defer conn.Close()

				// 保持连接
				for {
					// 发送和接收消息
					err := conn.WriteMessage(websocket.TextMessage, []byte(`{"topic": "topicA","id": "1"}`))
					if err != nil {
						fmt.Println("发送消息失败:", err)
						return
					}

					time.Sleep(1 * time.Second)
					mt, data, err := conn.ReadMessage()
					if err != nil {
						fmt.Println("读取消息失败:", err)
						return
					} else {
						fmt.Println("收到消息:", mt, string(data))
					}

					time.Sleep(1 * time.Second) // 每隔1秒发送一次消息
				}
			}(url)
		}
	}

	wg.Wait()
	fmt.Println("所有连接已关闭")
}
