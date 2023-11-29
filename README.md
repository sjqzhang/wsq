## API


### subscribe
- /ws
```json
 {
  "topic": "topicA",
  "id": "1"
}
```



### publish
- /ws/api 
```json
{
  "action": "demoString",
  "topic": "topicA",
  "id": "1",
  "message": {
    "data": "",
    "message": "ok",
    "code": 0
  },
  "header": {
    "demoKey": "demoString"
  }
}

```


### request/response
- /ws
```json
{"action":"request","topic":"proxy","id":"uuid","message": {
  "url": "https://www.baidu.com",
  "method": "get",
  "headers": {},
  "body": {}
}}
```



## 授权
```bash
curl --location 'http://127.0.0.1:8866/ws/login' \
--header 'Content-Type: application/json' \
--data '{
"username":"hello",
"password":"world"

}'
```
## 访问
```bash
curl --location 'http://127.0.0.1:8866/v2/alert' \
--header 'Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE3MDEyNDM4ODMsImlkIjoiYW5vbnltb3VzIiwibmFtZSI6ImFub255bW91cyIsIm9yaWdfaWF0IjoxNzAxMjQwMjgzLCJyb2xlIjoiIn0.QMKCJDTHLMxWboAleELliu8kCJAO8Ze6n7-gjtQlutk'
```


### reload proxy
```bash
kill -HUP pid
```
