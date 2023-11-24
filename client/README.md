
### flask app
## 授权
```json
curl --location 'http://127.0.0.1:5001/auth' \
--header 'Content-Type: application/json' \
--data '{

"username":"hello",
"password":"world"

}'
```
## 访问
```json
curl --location 'http://127.0.0.1:5001/api/user' \
--header 'Authorization: JWT eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJleHAiOjE3MDEwOTg2MzksImlhdCI6MTcwMDc5ODYzOSwibmJmIjoxNzAwNzk4NjM5LCJpZGVudGl0eSI6ImhlbGxvIn0.lhBCzj1ey_iG5COwmdSD8jQ9DmK7sBKAkaB6mK8iAy0' 
```
