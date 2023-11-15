package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/RussellLuo/timingwheel"
	"github.com/alicebob/miniredis/v2"
	mapset "github.com/deckarep/golang-set"
	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/jinzhu/gorm"
	_ "github.com/mattn/go-sqlite3"
	"github.com/redis/go-redis/v9"
	"github.com/sjqzhang/bus"
	"github.com/spf13/viper"
	"gopkg.in/natefinch/lumberjack.v2"
	"gopkg.in/yaml.v3"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"

	"strings"
	"sync"
	"time"
)

const WEBSOCKET_MESSAGE = "WEBSOCKET_MESSAGE"
const WEBSOCKET_EVENT_ClOSE = "WEBSOCKET_EVENT_ClOSE"

var hubLocal *hub
var logger *log.Logger

var db *gorm.DB

var config Config

var rdb *redis.Client

var rs *miniredis.Miniredis

type Response struct {
	Code int         `json:"retcode"`
	Data interface{} `json:"data"`
	Msg  string      `json:"message"`
}

func response(code int, data interface{}, msg string) []byte {
	resp := Response{
		Code: code,
		Data: data,
		Msg:  msg,
	}
	res, _ := json.Marshal(resp)

	return res
}

type WSMessage struct {
	MessageType int    `json:"message_type"`
	Data        []byte `json:"data"`
}

type Conn struct {
	sync.RWMutex
	*websocket.Conn
	send       chan WSMessage
	isClose    bool
	createTime int64
}

type CommonMap struct {
	sync.RWMutex
	m map[string]interface{}
}

func NewCommonMap() *CommonMap {
	return &CommonMap{
		m: make(map[string]interface{}),
	}
}

func (m *CommonMap) Store(key string, value interface{}) {
	m.Lock()
	defer m.Unlock()
	m.m[key] = value
}

// remove
func (m *CommonMap) Delete(key string) {
	m.Lock()
	defer m.Unlock()
	delete(m.m, key)
}

// get
func (m *CommonMap) Load(key string) (interface{}, bool) {
	m.RLock()
	defer m.RUnlock()
	v, ok := m.m[key]
	return v, ok
}

// get all from copy

func (m *CommonMap) LoadAll() map[string]interface{} {
	m.RLock()
	defer m.RUnlock()
	r := make(map[string]interface{})
	for k, v := range m.m {
		r[k] = v
	}
	return r
}

type NocIncident struct {
	ID              int             `json:"id"`
	IncidentID      string          `json:"incident_id"`
	Title           string          `json:"title"`
	StartTime       int64           `json:"start_time"`
	EndTime         int64           `json:"end_time"`
	Duration        int             `json:"duration"`
	EscalationTime  int64           `json:"escalation_time"`
	Region          json.RawMessage `json:"region" gorm:"region"`
	ProductLine     string          `json:"product_line"`
	Lvl2Team        string          `json:"lvl2_team"`
	Lvl3Team        string          `json:"lvl3_team"`
	Metric          string          `json:"metric"`
	Record          json.RawMessage `json:"record" gorm:"record"`
	ServiceCmdbName string          `json:"service_cmdb_name"`
	Operator        string          `json:"operator"`
	ReportURL       string          `json:"report_url"`
	GroupName       string          `json:"group_name"`
}

type Alert struct {
	ID          int             `json:"id"`
	EventId     string          `json:"event_id"`
	EventStatus string          `json:"event_status"`
	Message     string          `json:"message"`
	RawMessage  json.RawMessage `json:"raw_message"`
	StartTime   int64           `json:"start_time"`
	EndTime     int64           `json:"end_time"`
}

type Config struct {
	Server struct {
		Port  int  `mapstructure:"port" yaml:"port"`
		Debug bool `mapstructure:"debug" yaml:"debug"`
	} `yaml:"server"`
	EmbedRedis struct {
		Addr     string `mapstructure:"addr" yaml:"addr"`
		Password string `mapstructure:"password" yaml:"password"`
		DB       int    `mapstructure:"db" yaml:"db"`
	} `yaml:"embedRedis"`
	Database struct {
		DbType string `mapstructure:"db_type" yaml:"db_type"`
		Dsn    string `mapstructure:"dsn" yaml:"dsn"`
	} `yaml:"database"`
	Redis struct {
		Addr     string `mapstructure:"addr" yaml:"addr"`
		Password string `mapstructure:"password" yaml:"password"`
		DB       int    `mapstructure:"db" yaml:"db"`
	} `yaml:"redis"`
}

func InitConfig() {
	// 设置配置文件名称和路径
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")

	// 读取配置文件
	err := viper.ReadInConfig()
	if err != nil {
		fmt.Println("Failed to read configuration file:", err)

		// 检查配置文件是否存在
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			// 如果配置文件不存在，则生成模板
			config = Config{
				Server: struct {
					Port  int  `mapstructure:"port" yaml:"port"`
					Debug bool `mapstructure:"debug" yaml:"debug"`
				}{
					Port:  8866,
					Debug: true,
				},
				EmbedRedis: struct {
					Addr     string `mapstructure:"addr" yaml:"addr"`
					Password string `mapstructure:"password" yaml:"password"`
					DB       int    `mapstructure:"db" yaml:"db"`
				}{
					Addr:     ":6380",
					Password: "",
					DB:       0,
				},
				Database: struct {
					DbType string `mapstructure:"db_type" yaml:"db_type"`
					Dsn    string `mapstructure:"dsn" yaml:"dsn"`
				}{
					DbType: "sqlite3",
					Dsn:    "test.db",
				},
				Redis: struct {
					Addr     string `mapstructure:"addr" yaml:"addr"`
					Password string `mapstructure:"password" yaml:"password"`
					DB       int    `mapstructure:"db" yaml:"db"`
				}{
					Addr:     "127.0.0.1:6380",
					Password: "",
					DB:       0,
				},
			}

			// 将配置数据转换为YAML格式
			configBytes, err := yaml.Marshal(&config)
			if err != nil {
				fmt.Println("Failed to generate configuration template:", err)
				return
			}

			// 将YAML数据写入配置文件
			err = os.WriteFile("config.yaml", configBytes, 0644)
			if err != nil {
				fmt.Println("Failed to write configuration template:", err)
				return
			}

			fmt.Println("Configuration file generated:", viper.ConfigFileUsed())
			fmt.Println("Please configure the file and restart the application.")
			return
		}
	}
	err = viper.Unmarshal(&config)
	if err != nil {
		panic(err)
	}

}

func InitHub() {
	hubLocal = newHub()
}

func InitDB() {
	var err error
	db, err = gorm.Open(config.Database.DbType, config.Database.Dsn)
	db.AutoMigrate(&NocIncident{})
	db.AutoMigrate(&Alert{})
	if err != nil {
		panic(err)
	}

}

func InitRedis() {

	rs = miniredis.NewMiniRedis()

	rs.StartAddr(config.EmbedRedis.Addr)

	if config.EmbedRedis.Password != "" {
		rs.RequireAuth(config.EmbedRedis.Password)
	}

	rs.DB(config.EmbedRedis.DB)

	if err := rs.Start(); err != nil {
		panic(err)
	}

	go func() {
		for {
			rs.FastForward(time.Second * 1)
			time.Sleep(time.Second * 1)
		}
	}()

	rdb = redis.NewClient(&redis.Options{
		Addr:     config.Redis.Addr,
		Password: config.Redis.Password,
		DB:       config.Redis.DB,
	})
}

var ts *timingwheel.TimingWheel

func init() {

	bus.Subscribe(WEBSOCKET_MESSAGE, 1, func(ctx context.Context, message interface{}) {
		if message == nil {
			return
		}
		if v, ok := message.(Subscription); ok {
			go hubLocal.SendMessage(v)
		}
	})
	ts = timingwheel.NewTimingWheel(time.Second, 3)
	ts.Start()
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// 校验请求的来源，可以根据需求自定义逻辑
		return true
	},
}

// 订阅消息的结构体
type Subscription struct {
	Action  string            `json:"action"`
	Topic   string            `json:"topic"`
	ID      string            `json:"id"`
	Message interface{}       `json:"message"`
	Header  map[string]string `json:"header"`
}

type Message struct {
	Topic       string            `json:"topic"`
	Message     interface{}       `json:"message"`
	ID          string            `json:"id"`
	CallbackURL string            `json:"callback_url"`
	Header      map[string]string `json:"header"`
}

type hub struct {
	subs  *CommonMap
	conns mapset.Set
	//reqs        sync.Map
	reqs        *CommonMap
	callbackURL string
}

func newHub() *hub {

	getOutboundIP := func() (string, error) {
		conn, err := net.Dial("udp", "8.8.8.8:80")
		if err != nil {
			return "", err
		}
		defer conn.Close()

		localAddr := conn.LocalAddr().(*net.UDPAddr)
		return localAddr.IP.String(), nil
	}

	svcAddr, err := getOutboundIP()
	if err != nil {
		panic(err)
	}
	svcAddr = fmt.Sprintf("http://%s:%v/ws/api", svcAddr, config.Server.Port)
	return &hub{
		subs:        NewCommonMap(),
		conns:       mapset.NewSet(),
		reqs:        NewCommonMap(),
		callbackURL: svcAddr,
	}
}

func (h *hub) SendMessage(subscription Subscription) {
	defer func() {
		if err := recover(); err != nil {
			log.Println(err)
		}
	}()
	if subscription.Action == "response" {
		reqKey := fmt.Sprintf("req_%v", subscription.ID)
		defer h.reqs.Delete(reqKey)
		if v, ok := h.reqs.Load(reqKey); ok {
			data, err := json.Marshal(subscription)
			if err != nil {
				return
			}
			v.(*Conn).send <- WSMessage{
				MessageType: websocket.TextMessage,
				Data:        data,
			}
		}
		return
	}
	key := fmt.Sprintf("%s_$_%s", subscription.Topic, subscription.ID)
	data, err := json.Marshal(subscription)
	if err != nil {
		logger.Println(err, subscription)
		return
	}
	msg := WSMessage{
		MessageType: websocket.TextMessage,
		Data:        data,
	}
	if m, ok := h.subs.Load(key); ok {
		for conn := range m.(mapset.Set).Iter() {
			conn.(*Conn).send <- msg
		}
	}

}

func (h *hub) Run() {
	var memStats runtime.MemStats
	for {
		runtime.ReadMemStats(&memStats)
		msg := fmt.Sprintf("Goroutines:%v,Cardinality:%v,Memory:%v", runtime.NumGoroutine(), h.conns.Cardinality(), memStats.Alloc/1024)
		fmt.Println(msg)
		if config.Server.Debug {
			logger.Println(msg)
		}
		time.Sleep(time.Second)
	}
}

func (h *hub) Subscribe(conn *Conn, subscription Subscription) {
	if subscription.Action == "request" {
		ctx := context.Background()
		reqKey := fmt.Sprintf("req_%v", subscription.ID)
		if subscription.ID == "" {
			logger.Print(fmt.Sprintf("request id is null,Subscribe: %v", subscription))
			conn.send <- WSMessage{
				MessageType: websocket.TextMessage,
				Data:        response(-1, subscription, "request id is null"),
			}
			return
		}
		if _, ok := h.reqs.Load(reqKey); ok {
			conn.send <- WSMessage{
				MessageType: websocket.TextMessage,
				Data:        response(-1, subscription, "request already exists"),
			}
			return
		}
		rdb.Pipelined(ctx, func(pipeliner redis.Pipeliner) error {
			msg := Message{
				Topic:       subscription.Topic,
				Message:     subscription.Message,
				ID:          subscription.ID,
				CallbackURL: h.callbackURL,
				Header:      subscription.Header,
			}

			data, err := json.Marshal(msg)
			if err != nil {
				logger.Println(err)
				return err
			}

			topic := fmt.Sprintf("Topic_%s", subscription.Topic)
			pipeliner.SAdd(ctx, "Topics", topic)
			pipeliner.LPush(ctx, subscription.Topic, data)
			pipeliner.Publish(ctx, topic, "")
			pipeliner.Expire(ctx, topic, 10*time.Minute)
			pipeliner.LTrim(ctx, subscription.Topic, 0, 1000)
			if _, err := pipeliner.Exec(ctx); err == nil {
				h.reqs.Store(reqKey, conn)
				ts.AfterFunc(time.Second*60, func() {
					conn.send <- WSMessage{
						MessageType: websocket.TextMessage,
						Data:        response(-1, subscription, "request timeout"),
					}
					h.reqs.Delete(reqKey)
				})
			}
			return nil
		})
		return
	}

	key := fmt.Sprintf("%s_$_%s", subscription.Topic, subscription.ID)
	if m, ok := h.subs.Load(key); ok {
		m.(mapset.Set).Add(conn)
		h.subs.Store(key, m)
	} else {
		m := mapset.NewSet()
		m.Add(conn)
		h.subs.Store(key, m)
	}
	h.conns.Add(conn)
}

// unsubscribe from a topic
func (h *hub) Unsubscribe(conn *Conn, subscription Subscription) {
	fmt.Println("closing", conn.RemoteAddr())
	key := fmt.Sprintf("%s_$_%s", subscription.Topic, subscription.ID)
	if m, ok := h.subs.Load(key); ok {
		m.(mapset.Set).Remove(conn)
		if m.(mapset.Set).Cardinality() == 0 {
			h.subs.Delete(key)
		} else {
			h.subs.Store(key, m)
		}
	}
}

// unsubscribe from a topic
func (h *hub) RemoveFailedConn(conn *Conn) {
	conn.isClose = true
	go func(conn *Conn) {
		defer func() {
			if err := recover(); err != nil {
				log.Println(err)
			}
		}()
		h.conns.Remove(conn)
		for k, v := range h.subs.LoadAll() {
			for _, con := range v.(mapset.Set).ToSlice() {
				c := con.(*Conn)
				if c == conn {
					if m, ok := v.(mapset.Set); ok {
						m.(mapset.Set).Remove(conn)
						h.subs.Store(k, m)
					}
				}
			}
		}
	}(conn)
}

func isNetError(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := err.(*websocket.CloseError); ok || err == websocket.ErrCloseSent {
		return true
	}
	if ne, ok := err.(*net.OpError); ok {
		var se *os.SyscallError
		if errors.As(ne, &se) {
			seStr := strings.ToLower(se.Error())
			if strings.Contains(seStr, "broken pipe") ||
				strings.Contains(seStr, "connection reset by peer") {
				return true
			}
		}
	}
	seStr := strings.ToLower(err.Error())
	if strings.Contains(seStr, "broken pipe") ||
		strings.Contains(seStr, "connection reset by peer") {
		return true
	}
	return false
}

func readMessages(conn *Conn) {

	defer func() {
		if err := recover(); err != nil {
			hubLocal.RemoveFailedConn(conn)
			return
		}
	}()
	defer conn.Close()
	for {
		// 读取客户端发来的消息
		//conn.SetReadDeadline(time.Now().Add(time.Second * 10))
		//conn.RLock()
		messageType, message, err := conn.ReadMessage()
		//conn.RUnlock()
		if err != nil {
			if isNetError(err) {
				hubLocal.RemoveFailedConn(conn)
				return
			}
			logger.Println(fmt.Sprintf("ReadMessage Error:%v", err))
			continue
		}

		switch messageType {

		case websocket.CloseMessage:
			return
		case websocket.PingMessage:
			conn.send <- WSMessage{
				MessageType: websocket.PongMessage,
				Data:        []byte("pong"),
			}
		case websocket.TextMessage:
			// 解析订阅消息
			var subscription Subscription
			err = json.Unmarshal(message, &subscription)
			if err != nil {
				logger.Println(fmt.Sprintf("Failed to parse subscription message:%v,err:%v", message, err))
				continue
			}
			handleMessages(conn, subscription)

			logger.Println(fmt.Sprintf("订阅消息:%v", subscription))

		}

	}
}

func writeMessages(con *Conn) {
	defer func() {
		if err := recover(); err != nil {
			hubLocal.RemoveFailedConn(con)
			return
		}
	}()
	tick := time.NewTicker(time.Second * 10)
	for {
		select {
		case message, ok := <-con.send:
			if !ok {
				return
			}
			if con.isClose {
				return
			}

			con.Lock()
			//con.SetWriteDeadline(time.Now().Add(time.Second * 2))
			err := con.WriteMessage(message.MessageType, message.Data)
			con.Unlock()
			if isNetError(err) {
				hubLocal.RemoveFailedConn(con)
			}
		case <-tick.C:
			//ping current connection
			if con.isClose {
				return
			}
			con.Lock()
			err := con.WriteMessage(websocket.PingMessage, []byte{})
			con.Unlock()
			if isNetError(err) {
				hubLocal.RemoveFailedConn(con)
			}

		}
	}

}

func handleMessages(conn *Conn, subscription Subscription) {
	hubLocal.Subscribe(conn, subscription)
} // 获取订阅

var addr = flag.String("addr", ":8866", "http service address")

func main() {

	InitConfig()
	InitDB()
	InitRedis()
	InitHub()

	logFile := &lumberjack.Logger{
		Filename:   "gin.log", // 日志文件名称
		MaxSize:    100,       // 每个日志文件的最大大小（以MB为单位）
		MaxBackups: 5,         // 保留的旧日志文件的最大个数
		MaxAge:     30,        // 保留的旧日志文件的最大天数
		Compress:   true,      // 是否压缩旧的日志文件
	}
	logger = log.New(logFile, "[WS] ", log.LstdFlags)
	go hubLocal.Run()
	router := gin.Default()
	router.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		Output: logFile,
	}))
	if config.Server.Debug {
		pprof.Register(router)
	}

	gin.DefaultErrorWriter = logFile
	wd, _ := os.Getwd()
	os.Chdir(wd + "/examples/message")
	router.GET("/", func(c *gin.Context) {
		body, err := ioutil.ReadFile("home.html")
		if err != nil {
			logger.Println(err)
			return
		}
		c.Writer.Write(body)

	})
	router.GET("/ws", func(c *gin.Context) {
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			logger.Println("Failed to upgrade connection:", err)
			return
		}
		con := &Conn{
			Conn:       conn,
			RWMutex:    sync.RWMutex{},
			send:       make(chan WSMessage, 1000),
			createTime: time.Now().Unix(),
		}
		go readMessages(con)
		go writeMessages(con)
	})

	router.GET("/ws/noc_incident", func(c *gin.Context) {
		var incidents []NocIncident
		// 取当天的数据
		// 取过去24小时的数据

		startTime := c.Query("start_time")

		endTime := c.Query("end_time")

		if startTime == "" {

			today := time.Now().UTC()
			todayStart := today.Add(time.Hour * -24)
			db.Where("start_time >= ? AND start_time < ?", todayStart.Unix(), today.Unix()).Find(&incidents)
			c.JSON(200, Response{
				Code: 0,
				Data: incidents,
				Msg:  "ok",
			})
		} else {
			if endTime == "" {
				endTime = startTime
			}
			db.Where("start_time >= ? AND start_time < ?", startTime, endTime).Find(&incidents)
			c.JSON(200, Response{
				Code: 0,
				Data: incidents,
				Msg:  "ok",
			})
		}

	})

	router.GET("/ws/alert", func(c *gin.Context) {
		var incidents []Alert
		// 取当天的数据
		// 取过去24小时的数据

		startTime := c.Query("start_time")

		endTime := c.Query("end_time")

		groupId := c.Query("group_id")

		groupWhere := ""
		if groupId != "" {
			groupWhere = "and (json_extract(raw_message, '$.group_ids') like '[" + groupId + ",%' or json_extract(raw_message, '$.group_ids') like '%," + groupId + ",%'  or json_extract(raw_message, '$.group_ids') like '%," + groupId + "]' or json_extract(raw_message, '$.group_ids') like '[" + groupId + "]')"
		}

		if startTime == "" {

			today := time.Now().UTC()
			todayStart := today.Add(time.Hour * -24)
			if groupWhere != "" {
				db.Debug().Where("start_time >= ? AND start_time <= ? and event_status='firing' "+groupWhere, todayStart.Unix(), today.Unix()).Find(&incidents)
			} else {
				db.Where("start_time >= ? AND start_time <= ? and event_status='firing'", todayStart.Unix(), today.Unix()).Find(&incidents)
			}
			c.JSON(200, Response{
				Code: 0,
				Data: incidents,
				Msg:  "ok",
			})
		} else {
			if endTime == "" {
				endTime = startTime
			}
			if groupWhere != "" {
				db.Where("start_time >= ? AND start_time <= ? and event_status='firing' "+groupWhere, startTime, endTime).Find(&incidents)
			} else {
				db.Where("start_time >= ? AND start_time <= ? and event_status='firing'", startTime, endTime).Find(&incidents)
			}
			c.JSON(200, Response{
				Code: 0,
				Data: incidents,
				Msg:  "ok",
			})
		}

	})

	router.POST("/ws/alert", func(c *gin.Context) {

		var incident Alert

		var subscription Subscription
		err := c.BindJSON(&incident)
		if err != nil {
			logger.Println("Failed to parse subscription message:", err)
			return
		}
		var oldIncident Alert
		if db.First(&oldIncident, "event_id=?", incident.EventId).Error != nil {
			db.Create(&incident)
		} else {
			if oldIncident.ID != 0 {
				incident.ID = oldIncident.ID
				db.Save(incident)
			}
		}
		subscription.Topic = "alert"
		subscription.Message = incident
		type Raw struct {
			GroupIds    []int64 `json:"group_ids"`
			EventStatus string  `json:"event_status"`
		}
		var raw Raw
		json.Unmarshal([]byte(incident.RawMessage), &raw)
		if raw.EventStatus == "firing" {
			logger.Println(fmt.Sprintf("订阅消息：%v", subscription))
			for _, groupId := range raw.GroupIds {
				subscription.ID = fmt.Sprintf("%v", groupId)
				bus.Publish(WEBSOCKET_MESSAGE, subscription)
			}
		}
		c.JSON(http.StatusOK, Response{
			Code: 0,
			Data: subscription,
			Msg:  "ok",
		})

	})

	router.POST("/ws/noc_incident", func(c *gin.Context) {

		var incident NocIncident

		var subscription Subscription
		err := c.BindJSON(&incident)
		if err != nil {
			logger.Println("Failed to parse subscription message:", err)
			return
		}
		var oldIncident NocIncident
		if db.First(&oldIncident, "incident_id=?", incident.IncidentID).Error != nil {
			db.Create(&incident)
		} else {
			if oldIncident.ID != 0 {
				incident.ID = oldIncident.ID
				db.Save(incident)
			}
		}
		subscription.Topic = "noc_incident"
		subscription.Message = incident
		logger.Println(fmt.Sprintf("订阅消息：%v", subscription))
		bus.Publish(WEBSOCKET_MESSAGE, subscription)
		c.JSON(http.StatusOK, Response{
			Code: 0,
			Data: subscription,
			Msg:  "ok",
		})

	})

	router.POST("/ws/api", func(c *gin.Context) {

		var subscription Subscription
		err := c.BindJSON(&subscription)
		if err != nil {
			logger.Println("Failed to parse subscription message:", err)
			return
		}
		logger.Println(fmt.Sprintf("发布消息：%v", subscription))
		bus.Publish(WEBSOCKET_MESSAGE, subscription)
		c.JSON(http.StatusOK, Response{
			Code: 0,
			Data: subscription,
			Msg:  "ok",
		})

	})

	router.Run(fmt.Sprintf(":%v", config.Server.Port))

}
