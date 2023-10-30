package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/redis/go-redis/v9"
	"log"
	"net/http"
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
	sub = redisClient.Subscribe(nil)
	q = make(chan string)
}

func handlerTest(msg Message) interface{} {
	return msg
}

func init()  {

	topicMap["test"] = handlerTest
	topicMap["test1"] = handlerTest
	topicMap["test2"] = handlerTest
}




func freshSubscribe() {
	for {

		topics, err := redisClient.SMembers(context.Background(), "Topics").Result()
		if err != nil {
			log.Println(err)
			continue
		}
		if len(topics) > 0 {
			sub.Subscribe(context.Background(), topics...)
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
	resp, err := http.Post(msg.CallbackURL, "application/json", bytes.NewBuffer(data))
	if err != nil {
		log.Println(err)
		return
	}
	defer resp.Body.Close()
}

func consumer() {
	for {
		topic := <-q
		message, err := redisClient.RPop(context.Background(), topic).Bytes()
		if err != nil {
			log.Println(err)
			continue
		}
		if len(message) == 0 {
			continue
		}
		handler := topicMap[topic]
		if handler == nil {
			continue
		}
		var msg Message
		json.Unmarshal(message, &msg)
		result := handler(msg)
		wrapResult(msg, result)
	}
}

func main() {
	go freshSubscribe()
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
