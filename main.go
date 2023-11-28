package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
	_ "github.com/mattn/go-sqlite3"
	"github.com/sjqzhang/requests"
	"gopkg.in/natefinch/lumberjack.v2"
	"io/ioutil"
	"net/http"
	"os"
	"time"
)

type Subscription struct {
	Action  string            `json:"action"`
	Topic   string            `json:"topic"`
	ID      string            `json:"id"`
	Message interface{}       `json:"message"`
	Header  map[string]string `json:"header"`
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

var db *gorm.DB

func InitDB() {
	var err error
	db, err = gorm.Open("sqlite3", "test.db")
	db.AutoMigrate(&NocIncident{})
	db.AutoMigrate(&Alert{})
	if err != nil {
		panic(err)
	}

}

var addr = flag.String("addr", ":8866", "http service address")

type Response struct {
	Code int         `json:"retcode"`
	Data interface{} `json:"data"`
	Msg  string      `json:"message"`
}

func main() {

	InitDB()

	logFile := &lumberjack.Logger{
		Filename:   "gin.log", // 日志文件名称
		MaxSize:    100,       // 每个日志文件的最大大小（以MB为单位）
		MaxBackups: 5,         // 保留的旧日志文件的最大个数
		MaxAge:     30,        // 保留的旧日志文件的最大天数
		Compress:   true,      // 是否压缩旧的日志文件
	}

	router := gin.Default()
	router.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		Output: logFile,
	}))
	gin.DefaultErrorWriter = logFile
	wd, _ := os.Getwd()
	os.Chdir(wd + "/examples/message")
	router.GET("/", func(c *gin.Context) {
		body, err := ioutil.ReadFile("home.html")
		if err != nil {
			return
		}
		c.Writer.Write(body)

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

	router.POST("/ws/alert", func(c *gin.Context) {

		var incident Alert

		var subscription Subscription
		err := c.BindJSON(&incident)
		if err != nil {

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

		requests.PostJson("http://127.0.0.1:8866/ws/api", subscription)

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

		requests.PostJson("http://127.0.0.1:8866/ws/api", subscription)

		c.JSON(http.StatusOK, Response{
			Code: 0,
			Data: subscription,
			Msg:  "ok",
		})

	})

	router.Run(fmt.Sprintf(":%v", "8867"))

}
