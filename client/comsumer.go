package main

import (
	"context"
	"encoding/json"
	"github.com/redis/go-redis/v9"
	"github.com/sjqzhang/requests"
	"log"
	"strings"
	"time"
)

var (
	redisClient *redis.Client
	sub         *redis.PubSub
	topicMap    = make(map[string]func(Message) interface{})
	q           chan string
)

type Message struct {
	Action      string      `json:"action"`
	Topic       string      `json:"topic"`
	Message     interface{} `json:"message"`
	ID          string      `json:"id"`
	CallbackURL string      `json:"callback_url"`
}

func init() {
	redisClient = redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:6380",
		DB:   0,
	})
	sub = redisClient.Subscribe(context.Background(), "Topic_*")
	q = make(chan string, 100)
}

func handlerTest(msg Message) interface{} {
	return msg
}

func handlerProxy(msg Message) interface{} {

	type Req struct {
		Method string          `json:"method"`
		Url    string          `json:"url"`
		Body   string          `json:"body"`
		Header requests.Header `json:"headers"`
	}

	var req Req

	var data []byte

	switch msg.Message.(type) {
	case string:
		data = []byte(msg.Message.(string))
	case []byte:
		data = msg.Message.([]byte)
	default:
		data, _ = json.Marshal(msg.Message)
	}

	err := json.Unmarshal(data, &req)
	if err != nil {
		log.Println(err)
		return err
	}
	resp, err := requests.Requests().Do(strings.ToUpper(req.Method), req.Url, req.Header, req.Body)
	if err != nil {
		return err.Error()
	}
	return resp.Text()

}

func init() {

	topicMap["test"] = handlerTest
	topicMap["test1"] = handlerTest
	topicMap["test2"] = handlerTest
	topicMap["proxy"] = handlerProxy
}

func freshSubscribe() {
	for {
		topics, err := redisClient.SMembers(context.Background(), "Topics").Result()
		if err != nil {
			log.Println(err)
			continue
		}
		if len(topics) > 0 {
			err := sub.Subscribe(context.Background(), topics...)
			if err != nil {
				log.Println(err)
			}
		}
		time.Sleep(1 * time.Second)
	}
}

func redisSubscribe() {
	for {
		message, err := sub.ReceiveMessage(context.Background())
		if err != nil {
			log.Println(err)
			continue
		}
		topic := message.Channel[len("Topic_"):]
		q <- topic
	}
}

func wrapResult(msg Message, result interface{}) {
	msg.Message = result
	msg.Action = "response"
	data, err := json.Marshal(msg)
	if err != nil {
		log.Println(err)
		return
	}
	_, err = requests.PostJson(msg.CallbackURL, string(data))
	if err != nil {
		log.Println(err)
		return
	}
}

func consumer() {
	for {
		topic := <-q
		for {
			message, err := redisClient.RPop(context.Background(), topic).Bytes()
			if err != nil || len(message) == 0 {
				log.Println(err)
				break
			}
			if handler, ok := topicMap[topic]; ok {
				var msg Message
				json.Unmarshal(message, &msg)
				result := handler(msg)
				wrapResult(msg, result)
			}
		}
	}
}

func main() {
	go freshSubscribe()
	time.Sleep(1 * time.Second)
	go redisSubscribe()

	threadPool := make(chan struct{}, 40)
	for i := 0; i < cap(threadPool); i++ {
		threadPool <- struct{}{}
	}

	for {
		select {
		case <-threadPool:
			go func() {
				consumer()
				<-threadPool
			}()
		default:
			consumer()
		}
	}
}
