package main

import (
	"flag"
	"fmt"
	"github.com/gorilla/websocket"
	"sync"
	"time"
)


var addr = flag.String("ip", "127.0.0.1", "http service address")

func main() {

	flag.Parse()
	url := fmt.Sprintf(  "ws://%v:8866/ws",*addr) // WebSocket服务器的URL
	numConnections := 30000          // 要创建的连接数量

	var wg sync.WaitGroup
	wg.Add(numConnections)

	for i := 0; i < numConnections; i++ {
		go func() {
			defer wg.Done()
			// 建立WebSocket连接
			conn, _, err := websocket.DefaultDialer.Dial(url, nil)
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
                   fmt.Println(mt, string(data))
				}

				time.Sleep(1 * time.Second) // 每隔1秒发送一次消息
			}
		}()
	}

	wg.Wait()

	time.Sleep(10*time.Minute)
	fmt.Println("所有连接已关闭")
}
