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

