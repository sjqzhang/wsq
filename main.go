package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/RussellLuo/timingwheel"
	"github.com/alicebob/miniredis/v2"
	"github.com/casbin/casbin/v2"
	fileadapter "github.com/casbin/casbin/v2/persist/file-adapter"
	mapset "github.com/deckarep/golang-set"
	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/jinzhu/gorm"
	_ "github.com/mattn/go-sqlite3"
	"github.com/redis/go-redis/v9"
	"github.com/sjqzhang/bus"
	"github.com/sjqzhang/requests"
	"github.com/spf13/viper"
	"gopkg.in/natefinch/lumberjack.v2"

	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"runtime"

	jwt "github.com/appleboy/gin-jwt/v2"
	"gopkg.in/yaml.v3"

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

type Policy struct {
	Ptype  string
	Role   string
	Path   string
	Method string
}

type Server struct {
	router *gin.Engine
	*http.Server
	sigChan chan os.Signal
	//transportOnce sync.Once
	transport *http.Transport
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

type CasbinMiddleware struct {
	enforcer    *casbin.Enforcer
	middleWare  func(c *gin.Context)
	policies    []Policy
	policiesMap mapset.Set
	policyChan  chan Policy
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

type ForwardConfig struct {
	Prefix      string `yaml:"prefix" mapstructure:"prefix"`
	Forward     string `yaml:"forward" mapstructure:"forward"`
	Default     bool   `yaml:"default" mapstructure:"default"`
	RequireAuth bool   `yaml:"require_auth" mapstructure:"require_auth"`
	User        struct {
		Username string `yaml:"username" mapstructure:"username"`
		Password string `yaml:"password" mapstructure:"password"`
	} `yaml:"user"`
	Header map[string]string `yaml:"header" mapstructure:"header"`
}

type Config struct {
	Server struct {
		Port             int    `mapstructure:"port" yaml:"port"`
		Prefix           string `mapstructure:"prefix" yaml:"prefix"`
		Debug            bool   `mapstructure:"debug" yaml:"debug"`
		SaveCasbinPolicy bool   `mapstructure:"save_casbin_policy" yaml:"save_casbin_policy"`
		EmbedWebsocket   bool   `mapstructure:"embed_websocket" yaml:"embed_websocket"`
		EnableAuth       bool   `mapstructure:"enable_auth" yaml:"enable_auth"`
	} `yaml:"server"`
	EmbedRedis struct {
		Addr     string `mapstructure:"addr" yaml:"addr"`
		Password string `mapstructure:"password" yaml:"password"`
		DB       int    `mapstructure:"db" yaml:"db"`
		Enable   bool   `mapstructure:"enable" yaml:"enable"`
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

	ForwardConfig []ForwardConfig `yaml:"forwardConfig"`
	Jwt           struct {
		Algorithm     string `mapstructure:"algorithm" yaml:"algorithm"`
		SigningKey    string `mapstructure:"signing_key" yaml:"signing_key"`
		Timeout       int    `mapstructure:"timeout" yaml:"timeout"`
		MaxRefresh    int    `yaml:"max_refresh" mapstructure:"max_refresh"`
		CookieDomain  string `mapstructure:"cookie_domain" yaml:"cookie_domain"`
		CookieName    string `mapstructure:"cookie_name" yaml:"cookie_name"`
		SendCookie    bool   `mapstructure:"send_cookie" yaml:"send_cookie"`
		TokenHeadName string `mapstructure:"token_head_name" yaml:"token_head_name"`
		TokenLookup   string `mapstructure:"token_lookup" yaml:"token_lookup"`
		Enable        bool   `mapstructure:"enable" yaml:"enable"`
	} `yaml:"jwt"`
	Casbin struct {
		ModelPath  string            `mapstructure:"model_path" yaml:"model_path"`
		PolicyPath string            `mapstructure:"policy_path" yaml:"policy_path"`
		UserPath   string            `mapstructure:"user_path" yaml:"user_path"`
		FieldMap   map[string]string `mapstructure:"field_map" yaml:"field_map"`
		Enable     bool              `mapstructure:"enable" yaml:"enable"`
	} `yaml:"casbin"`
}

func InitConfig() (*Config, error) {
	// 设置配置文件名称和路径

	var conf Config
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./conf")
	viper.SetConfigFile("./conf/config.yaml")

	// 读取配置文件
	err := viper.ReadInConfig()
	if err != nil {

		// 检查配置文件是否存在
		if os.IsNotExist(err) {
			// 如果配置文件不存在，则生成模板
			conf = Config{
				Server: struct {
					Port             int    `mapstructure:"port" yaml:"port"`
					Prefix           string `mapstructure:"prefix" yaml:"prefix"`
					Debug            bool   `mapstructure:"debug" yaml:"debug"`
					SaveCasbinPolicy bool   `mapstructure:"save_casbin_policy" yaml:"save_casbin_policy"`
					EmbedWebsocket   bool   `mapstructure:"embed_websocket" yaml:"embed_websocket"`
					EnableAuth       bool   `mapstructure:"enable_auth" yaml:"enable_auth"`
				}{
					Port:             8866,
					Prefix:           "/ws",
					Debug:            true,
					SaveCasbinPolicy: true,
					EmbedWebsocket:   true,
					EnableAuth:       true,
				},
				EmbedRedis: struct {
					Addr     string `mapstructure:"addr" yaml:"addr"`
					Password string `mapstructure:"password" yaml:"password"`
					DB       int    `mapstructure:"db" yaml:"db"`
					Enable   bool   `mapstructure:"enable" yaml:"enable"`
				}{
					Addr:     ":6380",
					Password: "",
					DB:       0,
					Enable:   true,
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
				ForwardConfig: []ForwardConfig{
					{
						Prefix:      "/v2",
						Forward:     "http://127.0.0.1:5000",
						Default:     true,
						RequireAuth: false,
						User: struct {
							Username string `yaml:"username" mapstructure:"username"`
							Password string `yaml:"password" mapstructure:"password"`
						}{
							Username: "admin",
							Password: "admin",
						},
						Header: map[string]string{},
					},
				},
				Jwt: struct {
					Algorithm     string `mapstructure:"algorithm" yaml:"algorithm"`
					SigningKey    string `mapstructure:"signing_key" yaml:"signing_key"`
					Timeout       int    `mapstructure:"timeout" yaml:"timeout"`
					MaxRefresh    int    `yaml:"max_refresh" mapstructure:"max_refresh"`
					CookieDomain  string `mapstructure:"cookie_domain" yaml:"cookie_domain"`
					CookieName    string `mapstructure:"cookie_name" yaml:"cookie_name"`
					SendCookie    bool   `mapstructure:"send_cookie" yaml:"send_cookie"`
					TokenHeadName string `mapstructure:"token_head_name" yaml:"token_head_name"`
					TokenLookup   string `mapstructure:"token_lookup" yaml:"token_lookup"`
					Enable        bool   `mapstructure:"enable" yaml:"enable"`
				}{
					Algorithm:     "HS256",
					SigningKey:    "hello",
					Timeout:       3600,
					MaxRefresh:    0,
					CookieDomain:  "",
					CookieName:    "jwt",
					TokenLookup:   "header: Authorization, query: token, cookie: jwt",
					TokenHeadName: "Bearer",
					SendCookie:    true,
					Enable:        true,
				},
				Casbin: struct {
					ModelPath  string            `mapstructure:"model_path" yaml:"model_path"`
					PolicyPath string            `mapstructure:"policy_path" yaml:"policy_path"`
					UserPath   string            `mapstructure:"user_path" yaml:"user_path"`
					FieldMap   map[string]string `mapstructure:"field_map" yaml:"field_map"`
					Enable     bool              `mapstructure:"enable" yaml:"enable"`
				}{
					ModelPath:  "conf/model.conf",
					PolicyPath: "conf/policy.csv",
					UserPath:   "conf/user.txt",
					FieldMap: map[string]string{
						"username": "username",
						"password": "password",
					},
					Enable: true,
				},
			}

			// 将配置数据转换为YAML格式
			configBytes, err := yaml.Marshal(&conf)
			if err != nil {
				fmt.Println(err)
				return nil, err
			}

			// 将YAML数据写入配置文件
			err = os.WriteFile("conf/config.yaml", configBytes, 0644)
			if err != nil {
				return nil, err
			}
			return &conf, nil
		}
		return nil, err
	}
	err = viper.Unmarshal(&conf)
	if err != nil {
		return nil, err
	}
	return &conf, nil

}

func InitHub() {
	hubLocal = newHub()
}

// 定义用户结构体
type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// 定义登录请求结构体
type Login struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// 定义 JWT 中间件的配置
var authMiddleware *jwt.GinJWTMiddleware

var casbinMiddle *CasbinMiddleware

var userChecker IGetUserByIDAndPassword

type IGetUserByIDAndPassword interface {
	getUserByIDAndPassword(userID, password string) (*User, error)
}

type FileGetUserByIDAndPasswordChecker struct {
}

type NetGetUserByIDAndPasswordChecker struct {
}

func (f *NetGetUserByIDAndPasswordChecker) getUserByIDAndPassword(userID, password string) (*User, error) {
	userFieldName := "username"
	pwdFieldName := "password"
	if v, ok := config.Casbin.FieldMap["username"]; ok {
		userFieldName = v
	}
	if v, ok := config.Casbin.FieldMap["password"]; ok {
		pwdFieldName = v
	}
	data := make(map[string]string)
	data[userFieldName] = userID
	data[pwdFieldName] = password
	dataBytes, err := json.Marshal(data)
	resp, err := requests.PostJson(config.Casbin.UserPath, string(dataBytes))
	if err != nil || resp.R.StatusCode != 200 {
		return nil, err
	}
	return &User{
		ID:   userID,
		Name: userID,
		Role: "",
	}, nil

}

// 模拟从数据库中根据用户ID和密码验证用户
func (f *FileGetUserByIDAndPasswordChecker) getUserByIDAndPassword(userID, password string) (*User, error) {
	// 从conf/user.txt中读取用户信息

	content, err := ioutil.ReadFile(config.Casbin.UserPath)
	if err != nil {
		logger.Println(err)
		return nil, err
	}

	users := strings.Split(string(content), "\n")

	for _, user := range users {
		userInfo := strings.Split(user, ",")
		if userInfo[0] == userID && userInfo[1] == password {
			return &User{
				ID:   userID,
				Name: userInfo[0],
				Role: "",
			}, nil
		}
	}

	return nil, fmt.Errorf("用户名或密码错误")

}

// 初始化 JWT 中间件和 Casbin 中间件
func InitJwt() {

	// JWT 中间件配置
	authMiddleware = &jwt.GinJWTMiddleware{
		Realm:         "test zone",
		Key:           []byte(config.Jwt.SigningKey),
		Timeout:       time.Second * time.Duration(config.Jwt.Timeout),
		MaxRefresh:    time.Hour * time.Duration(config.Jwt.MaxRefresh),
		IdentityKey:   "id",
		SendCookie:    config.Jwt.SendCookie,
		CookieName:    config.Jwt.CookieName,
		CookieDomain:  config.Jwt.CookieDomain,
		TokenLookup:   config.Jwt.TokenLookup,
		TokenHeadName: config.Jwt.TokenHeadName,
		PayloadFunc: func(data interface{}) jwt.MapClaims {
			if v, ok := data.(*User); ok {
				return jwt.MapClaims{
					"id":   v.ID,
					"name": v.Name,
					"role": v.Role,
				}
			}
			return jwt.MapClaims{}
		},
		IdentityHandler: func(c *gin.Context) interface{} {
			claims := jwt.ExtractClaims(c)

			return &User{
				ID:   claims["id"].(string),
				Name: claims["name"].(string),
				Role: claims["role"].(string),
			}

		},
		Authenticator: func(c *gin.Context) (interface{}, error) {
			var loginVals Login
			if err := c.ShouldBind(&loginVals); err != nil {
				return "", jwt.ErrMissingLoginValues
			}
			userID := loginVals.Username
			password := loginVals.Password

			// 根据用户ID和密码验证用户
			user, err := userChecker.getUserByIDAndPassword(userID, password)
			if err != nil {
				return nil, jwt.ErrFailedAuthentication
			}

			return user, nil
		},
		Authorizator: func(data interface{}, c *gin.Context) bool {
			if user, ok := data.(*User); ok {
				// 在这里根据用户的角色和请求路径进行权限验证
				// 返回 true 表示允许访问该路径，返回 false 表示拒绝访问该路径
				// 示例中只做了简单的角色验证，您可以根据实际需求进行自定义
				_ = user
				return true
			}
			return false
		},
		Unauthorized: func(c *gin.Context, code int, message string) {
			c.JSON(code, gin.H{
				"code":    code,
				"message": message,
				"data":    nil,
			})
		},
		TimeFunc: time.Now,
		LoginResponse: func(c *gin.Context, code int, message string, time time.Time) {
			c.JSON(code, gin.H{
				"code":    code,
				"data":    message,
				"message": "ok",
			})
		},
		LogoutResponse: func(c *gin.Context, code int) {
			c.JSON(code, gin.H{
				"code": code,
			})
		},
		RefreshResponse: func(c *gin.Context, code int, message string, time time.Time) {
			c.JSON(code, gin.H{
				"code":    code,
				"message": message,
			})
		},
		HTTPStatusMessageFunc: func(e error, c *gin.Context) string {
			return e.Error()
		},
	}

	if err := authMiddleware.MiddlewareInit(); err != nil {
		panic(err)
	}

}

func InitDB() {
	var err error
	db, err = gorm.Open(config.Database.DbType, config.Database.Dsn)
	if err != nil {
		panic(err)
	}

}

var server *Server

func InitServer() {

	router := gin.Default()
	router.Use(Logger())
	routerGroup := router.Group(config.Server.Prefix)

	logFile := &lumberjack.Logger{
		Filename:   "log/gin.log", // 日志文件名称
		MaxSize:    100,           // 每个日志文件的最大大小（以MB为单位）
		MaxBackups: 5,             // 保留的旧日志文件的最大个数
		MaxAge:     30,            // 保留的旧日志文件的最大天数
		Compress:   true,          // 是否压缩旧的日志文件
	}
	logger = log.New(logFile, "[WS] ", log.LstdFlags)
	go hubLocal.Run()

	routerGroup.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		Output: logFile,
	}))
	if config.Server.Debug {
		pprof.Register(router)
	}

	gin.DefaultErrorWriter = logFile
	wd, _ := os.Getwd()
	os.Chdir(wd + "/examples/message")

	server = &Server{
		router: router,
		Server: &http.Server{
			Addr:    fmt.Sprintf(":%v", config.Server.Port),
			Handler: router,
		},
		sigChan: make(chan os.Signal, 1),
		transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			Proxy: http.ProxyFromEnvironment,
		},
	}

	if err := InitRouter(server, router, routerGroup, config); err != nil {
		panic(err)
	}

	signal.Notify(server.sigChan, syscall.SIGHUP)

	go server.ListenAndServe()

	// 监听重载信号
	go func() {
		for {
			sig := <-server.sigChan
			if sig == syscall.SIGHUP {
				log.Println("Received reload signal. Reloading...")
				server.Reload(server, router)
			}
			if sig == syscall.SIGINT || sig == syscall.SIGTERM {
				if config.Server.Debug {
					os.Exit(0)
				}
			}
		}
	}()

	// 等待中断信号
	signalChan := make(chan os.Signal, 1)
	signal.Notify(server.sigChan, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, syscall.SIGINT, syscall.SIGHUP)
	<-signalChan

	// 关闭 HTTP 服务器
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Fatal("Server shutdown:", err)
	}
	log.Println("Server exiting")
}

func InitRouter(server *Server, router *gin.Engine, routerGroup *gin.RouterGroup, config Config) (errorInitRouter error) {

	defer func() {
		if e := recover(); e != nil {
			errorInitRouter = fmt.Errorf("%v", e)
			logger.Println(errorInitRouter)
		}
	}()

	if config.Server.EmbedWebsocket {

		if config.Server.Prefix != "" && config.Server.Prefix != "/" {
			router.GET("/", func(c *gin.Context) {
				body, err := ioutil.ReadFile("home.html")
				if err != nil {
					logger.Println(err)
					return
				}
				c.Writer.Write(body)

			})
		}
		routerGroup.GET("", func(c *gin.Context) {
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

		router.Use(func(c *gin.Context) {
			//c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
			//c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			//c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
			//c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE, PATCH")
			policy := Policy{
				Ptype:  "p",
				Role:   "admin_role",
				Path:   c.Request.RequestURI,
				Method: c.Request.Method,
			}
			casbinMiddle.policyChan <- policy
			c.Next()
		})
		routerGroup.POST("/api", server.HandlerWebSocketResponse)
	}

	var middlewares []gin.HandlerFunc
	if config.Jwt.Enable {
		middlewares = append(middlewares, authMiddleware.MiddlewareFunc())
	}
	if config.Casbin.Enable {
		middlewares = append(middlewares, casbinMiddle.middleWare)
	}
	middlewares = append(middlewares, server.HandlerNoRoute)
	router.NoRoute(middlewares...)
	if config.Server.EnableAuth {
		router.POST("/login", authMiddleware.LoginHandler)
		router.POST("/refresh_token", authMiddleware.RefreshHandler)
		router.POST("/logout", authMiddleware.LogoutHandler)
	}

	for _, cfg := range config.ForwardConfig {
		forwardCfg := cfg
		prefix := forwardCfg.Prefix
		if !strings.HasPrefix(prefix, "/") {
			prefix = "/" + prefix
		}
		//targetURL := forwardCfg.Forward

		var middlewares []gin.HandlerFunc
		if forwardCfg.RequireAuth {
			if forwardCfg.User.Username != "" {
				middlewares = append(middlewares, gin.BasicAuth(gin.Accounts{
					forwardCfg.User.Username: forwardCfg.User.Password,
				}))
			} else {
				middlewares = append(middlewares, authMiddleware.MiddlewareFunc())
				middlewares = append(middlewares, casbinMiddle.middleWare)
			}
		}
		middlewares = append(middlewares, func(c *gin.Context) {
			// 创建反向代理

			if isWebSocketRequest(c.Request) {
				server.proxyWebSocket(c, forwardCfg)
				return
			}

			server.proxyHTTP(c, forwardCfg)

			//uri, err := url.Parse(targetURL)
			//if err != nil {
			//	logger.Println(err)
			//	c.Writer.Write([]byte(err.Error()))
			//	return
			//}
			//proxy := httputil.NewSingleHostReverseProxy(uri)
			//for k, v := range forwardCfg.Header {
			//	c.Request.Header.Set(k, v)
			//}
			//// 更改请求的主机头
			//c.Request.Host = uri.Host
			//c.Request.URL.Path = strings.TrimPrefix(strings.TrimPrefix(c.Request.URL.Path, forwardCfg.Prefix), uri.Path)
			//c.Request.RequestURI = c.Request.URL.Path
			//
			//// 将请求转发到目标URL
			//proxy.ServeHTTP(c.Writer, c.Request)
		})

		// 注册转发路由
		// if prefix == "/" || prefix == "" {
		// 	cfg, _ := json.Marshal(forwardCfg)
		// 	msg := fmt.Sprintf("(ERROR)prefix can't be empty.\nforwardCfg:%v", string(cfg))
		// 	logger.Println(msg)
		// 	panic(msg)
		// }
		router.Any(prefix+"/*path", middlewares...)
	}
	return errorInitRouter
}

func InitRedis() {

	if !config.EmbedRedis.Enable {
		return
	}

	rs = miniredis.NewMiniRedis()

	if config.EmbedRedis.Password != "" {
		rs.RequireAuth(config.EmbedRedis.Password)
	}

	rs.DB(config.EmbedRedis.DB)

	if err := rs.StartAddr(config.EmbedRedis.Addr); err != nil {
		//panic(err)
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

func InitCasbin() {

	casbinMiddle = &CasbinMiddleware{
		policies:    make([]Policy, 0),
		policiesMap: mapset.NewSet(),
		policyChan:  make(chan Policy, 1000),
	}

	// Casbin 中间件配置
	modelPath := config.Casbin.ModelPath
	policyPath := config.Casbin.PolicyPath

	// load policy from file to server's policies
	var policies []Policy
	var policiesRole []Policy
	data, err := ioutil.ReadFile(policyPath)
	if err != nil {
		panic(err)
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		parts := strings.Split(line, ",")
		for i, p := range parts {
			parts[i] = strings.TrimSpace(p)
		}
		if len(parts) == 3 {
			key := fmt.Sprintf("%s,%s,%s", parts[0], parts[1], parts[2])
			if casbinMiddle.policiesMap.Contains(key) {
				continue
			} else {
				casbinMiddle.policiesMap.Add(key)
			}
			policiesRole = append(policiesRole, Policy{
				Ptype: parts[0],
				Role:  parts[1],
				Path:  parts[2],
			})
		}
		if len(parts) == 4 {
			key := fmt.Sprintf("%s,%s,%s,%s", parts[0], parts[1], parts[2], parts[3])
			if casbinMiddle.policiesMap.Contains(key) {
				continue
			} else {
				casbinMiddle.policiesMap.Add(key)
			}
			policies = append(policies, Policy{
				Ptype:  parts[0],
				Role:   parts[1],
				Path:   parts[2],
				Method: parts[3],
			})
		}
	}
	casbinMiddle.policies = policies

	adapter := fileadapter.NewAdapter(policyPath)

	e, err := casbin.NewEnforcer(modelPath, adapter)

	if err != nil {
		panic(err)
	}

	go func() {
		for policy := range casbinMiddle.policyChan {
			if !config.Server.SaveCasbinPolicy {
				continue
			}
			key := fmt.Sprintf("%s,%s,%s,%s", policy.Ptype, policy.Role, policy.Path, policy.Method)
			if casbinMiddle.policiesMap.Contains(key) {
				continue
			}
			casbinMiddle.policiesMap.Add(policy)
			casbinMiddle.policies = append(casbinMiddle.policies, policy)
			e.AddPolicy(policy.Role, policy.Path, policy.Method)

			// 保存casbinMiddle.policies策略到文件
			var pp []string
			for _, p := range casbinMiddle.policies {
				if p.Method == "" {
					pp = append(pp, fmt.Sprintf("%s,%s,%s", p.Ptype, p.Role, p.Path))
				} else {
					pp = append(pp, fmt.Sprintf("%s,%s,%s,%s", p.Ptype, p.Role, p.Path, p.Method))
				}

			}
			for _, p := range policiesRole {
				pp = append(pp, fmt.Sprintf("%s,%s,%s", p.Ptype, p.Role, p.Path))
			}
			ioutil.WriteFile(policyPath, []byte(strings.Join(pp, "\n")), 0644)
		}
	}()

	casbinMiddle.enforcer = e
	casbinMiddle.middleWare = func(c *gin.Context) {
		// 获取用户角色

		username := "anonymous"

		if user, ok := c.Get(authMiddleware.IdentityKey); ok {
			username = user.(*User).Name
		}

		// 检查用户的权限
		if ok, err := e.Enforce(username, c.Request.RequestURI, c.Request.Method); !ok || err != nil {
			// 如果用户没有访问权限，返回错误信息
			c.JSON(http.StatusForbidden, gin.H{
				"error": "You don't have permission to access this resource",
			})
			c.Abort()
			return
		}

		c.Next()
	}

}

func init() {

	//判断conf文件夹是否存在，不存在则创建
	if _, err := os.Stat("conf"); os.IsNotExist(err) {
		os.Mkdir("conf", os.ModePerm)
	}
	//自动生成model.conf,policy.conf文件
	model_conf := `[request_definition]
r = sub, obj, act

[policy_definition]
p = sub, obj, act

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub) && r.obj == p.obj && r.act == p.act


`
	policy_conf := `p, read_role, /protected, GET
p, write_role, /api/user,GET
p, read_role, /ws/alert,GET
g, admin_role, read_role
g, admin_role,write_role
g, admin,admin_role
g,anonymous,read_role
`
	user_txt := `anonymous,anonymous`
	if _, err := os.Stat("conf/model.conf"); os.IsNotExist(err) {
		ioutil.WriteFile("conf/model.conf", []byte(model_conf), 0644)
	}
	if _, err := os.Stat("conf/policy.csv"); os.IsNotExist(err) {
		ioutil.WriteFile("conf/policy.csv", []byte(policy_conf), 0644)
	}
	if _, err := os.Stat("conf/user.txt"); os.IsNotExist(err) {
		ioutil.WriteFile("conf/user.txt", []byte(user_txt), 0644)
	}

	//判断log文件夹是否存在，不存在则创建
	if _, err := os.Stat("log"); os.IsNotExist(err) {
		os.Mkdir("log", os.ModePerm)
	}

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

	conf, err := InitConfig()
	if err != nil {
		panic(err)
	}
	config = *conf

	if strings.HasPrefix(config.Casbin.UserPath, "http") {
		userChecker = &NetGetUserByIDAndPasswordChecker{}
	} else {
		userChecker = &FileGetUserByIDAndPasswordChecker{}
	}

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
		if config.Server.Debug {
			fmt.Println(msg)
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
					if _, ok := h.reqs.Load(reqKey); ok {
						conn.send <- WSMessage{
							MessageType: websocket.TextMessage,
							Data:        response(-1, subscription, "request timeout"),
						}
						h.reqs.Delete(reqKey)
					}
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
			data, err := json.Marshal(subscription)
			if err != nil {
				logger.Println(err)
			} else {
				logger.Println(fmt.Sprintf("subscribe message:%v", string(data)))
			}

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

func Logger() gin.HandlerFunc {
	// 创建日志输出器
	logOutput := &lumberjack.Logger{
		Filename:   "log/access.log",
		MaxSize:    100, // 单位：MB
		MaxBackups: 3,
		MaxAge:     30,    // 单位：天
		Compress:   false, // 是否压缩日志文件
	}

	//// 设置日志输出到文件
	//log.SetOutput(logOutput)

	return func(c *gin.Context) {
		// 开始时间
		start := time.Now()

		// 处理请求
		c.Next()

		username := "anonymous"

		if user, ok := c.Get(authMiddleware.IdentityKey); ok {
			username = user.(*User).Name
		}

		// 结束时间
		end := time.Now()

		// 请求IP地址
		clientIP := c.ClientIP()

		// 请求方法
		method := c.Request.Method

		// 请求路径
		path := c.Request.URL.Path

		// 响应状态码
		statusCode := c.Writer.Status()

		// 响应大小
		size := c.Writer.Size()

		// 请求耗时
		latency := end.Sub(start)

		// 构建日志条目
		logEntry := fmt.Sprintf("%s -[%s]- [%s] \"%s %s\" %d %d %s\n",
			clientIP,
			username,
			end.Format("2006-01-02:15:04:05 -0700"),
			method,
			path,
			statusCode,
			size,
			latency.String(),
		)

		// 输出日志
		logOutput.Write([]byte(logEntry))
	}
}

var addr = flag.String("addr", ":8866", "http service address")

func (s *Server) HandlerNoRoute(c *gin.Context) {
	// 检查是否有匹配的 WebSocket 代理配置
	for _, forwardCfg := range config.ForwardConfig {
		if forwardCfg.Default {
			// 检查请求是否是 WebSocket 连接
			if isWebSocketRequest(c.Request) {
				// 进行 WebSocket 代理
				s.proxyWebSocket(c, forwardCfg)
				return
			}
		}
	}

	// 检查是否有匹配的 HTTP 代理配置
	for _, forwardCfg := range config.ForwardConfig {
		if forwardCfg.Default {
			// 进行 HTTP 代理
			s.proxyHTTP(c, forwardCfg)
			return
		}
	}

	// 如果没有匹配的代理配置，返回 404 Not Found
	c.JSON(http.StatusNotFound, gin.H{
		"code": 404,
		"msg":  "not found",
	})
}

// 检查请求是否是 WebSocket 连接
func isWebSocketRequest(req *http.Request) bool {
	return strings.ToLower(req.Header.Get("Connection")) == "upgrade" &&
		strings.ToLower(req.Header.Get("Upgrade")) == "websocket"
}

// WebSocket 代理
func (s *Server) proxyWebSocket(c *gin.Context, forwardCfg ForwardConfig) {
	// 创建 WebSocket 连接
	//upgrader := websocket.Upgrader{}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Println("WebSocket upgrade failed:", err)
		return
	}
	defer conn.Close()

	// 创建自定义的 Dialer
	dialer := &websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,             // 使用 ProxyFromEnvironment 获取代理配置
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true}, // 忽略证书验证
		HandshakeTimeout: 45 * time.Second,
	}

	// 连接到目标 WebSocket 服务器 replace http:// -> ws:// and replace https:// -> wss://

	forwardURL := strings.Replace(forwardCfg.Forward, "https://", "wss://", 1)
	forwardURL = strings.Replace(forwardURL, "http://", "ws://", 1)

	targetURL, _ := url.Parse(forwardURL)
	targetConn, _, err := dialer.Dial(targetURL.String(), nil)
	if err != nil {
		log.Println("Failed to connect to target WebSocket server:", err)
		return
	}
	defer targetConn.Close()

	// 在两个连接之间进行消息转发
	go func() {
		for {
			messageType, message, err := conn.ReadMessage()
			if err != nil {
				log.Println("Failed to read message from client:", err)
				return
			}
			err = targetConn.WriteMessage(messageType, message)
			if err != nil {
				log.Println("Failed to send message to target server:", err)
				return
			}
		}
	}()

	for {
		messageType, message, err := targetConn.ReadMessage()
		if err != nil {
			log.Println("Failed to read message from target server:", err)
			return
		}
		err = conn.WriteMessage(messageType, message)
		if err != nil {
			log.Println("Failed to send message to client:", err)
			return
		}
	}
}

// HTTP 代理
func (s *Server) proxyHTTP(c *gin.Context, forwardCfg ForwardConfig) {
	targetURL, err := url.Parse(forwardCfg.Forward)
	if err != nil {
		log.Println("Failed to parse target URL:", err)
		return
	}
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = targetURL.Scheme
			req.URL.Host = targetURL.Host
			if !strings.HasSuffix(targetURL.Path, "/") {
				targetURL.Path = targetURL.Path + "/"
			}
			old:=req.URL.Path
			req.URL.Path = targetURL.Path + strings.TrimPrefix(req.URL.Path, forwardCfg.Prefix)
			req.URL.Path=strings.Replace(req.URL.Path,"//","/",-1)
			if config.Server.Debug {
				fmt.Println(fmt.Sprintf("URL Rewrite:%v -> %v", old,req.URL.Path))
			}
			for k, v := range forwardCfg.Header {
				req.Header.Set(k, v)
			}
			req.Host = targetURL.Host
		},
		Transport: s.transport, // 使用共享的 http.Transport 实例
	}
	proxy.ServeHTTP(c.Writer, c.Request)
}

func (s *Server) HandlerWebSocketResponse(c *gin.Context) {
	var subscription Subscription
	err := c.BindJSON(&subscription)
	if err != nil {
		logger.Println("Failed to parse subscription message:", err)
		return
	}
	logger.Println(fmt.Sprintf("publish message：%v", subscription))
	bus.Publish(WEBSOCKET_MESSAGE, subscription)
	c.JSON(http.StatusOK, Response{
		Code: 0,
		Data: subscription,
		Msg:  "ok",
	})
}

func (s *Server) Run() {

	s.router.Run(fmt.Sprintf(":%v", config.Server.Port))

}

func (s *Server) Reload(server *Server, router *gin.Engine) {
	// 创建一个新的 Gin 引擎实例
	InitJwt()
	InitCasbin()
	newEngine := gin.Default()
	if conf, err := InitConfig(); err != nil {
		logger.Println(fmt.Sprintf("(ERROR) Reload Failed:%v", err))
		return
	} else {
		logger.Println(fmt.Sprintf("Reload Config:%v", conf))
		newEngine.Use(Logger())
		routerGroup := newEngine.Group(config.Server.Prefix)
		server.router = newEngine
		if err := InitRouter(server, newEngine, routerGroup, *conf); err != nil {
			msg := fmt.Sprintf("(ERROR) Reload Failed:%v", err)
			fmt.Println(msg)
			logger.Println(msg)
			return
		} else {
			config = *conf
			server.Close()
		}

	}
	server.Server = &http.Server{
		Addr:    fmt.Sprintf(":%v", config.Server.Port),
		Handler: newEngine,
	}
	go func() {
		if err := server.ListenAndServe(); err != nil {
			logger.Println(err)

		} else {
			log.Println("Reload completed.")
		}
		time.Sleep(time.Millisecond * 100)

	}()
}

func main() {
	InitDB()
	InitRedis()
	InitHub()
	InitCasbin()
	InitJwt()
	InitServer()
}
