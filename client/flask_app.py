#! -*- encoding: utf-8 -*-
from flask import Flask, request,g
from casbin import Enforcer
from flask_jwt import JWT, jwt_required, current_identity
from werkzeug.security import safe_str_cmp
from functools import wraps
from datetime import datetime, timedelta

app = Flask(__name__,instance_path='/tmp')
app.config['SECRET_KEY'] = 'supersecretkey'
app.config['JWT_EXPIRATION_DELTA']=timedelta(seconds=300000)

# 加载Casbin的model和policy
enforcer = Enforcer("model.conf", "policy.csv")

# 加载用户文件
users = {}
with open("user.txt") as f:
    for line in f:
        username, password = line.strip().split(",")
        users[username] = password

# 用户认证回调函数
class User(object):
    def __init__(self, id):
        self.id = id

def authenticate(username, password):
    if username in users and safe_str_cmp(users.get(username).encode('utf-8'), password.encode('utf-8')):
        return User(username)

# 根据用户ID获取用户对象
def identity(payload):
    user_id = payload['identity']
    return user_id

jwt = JWT(app, authenticate, identity)



def authorize():

    def decorator(func):
        @jwt_required()
        @wraps(func)
        def wrapper(*args, **kwargs):
            # 获取当前用户的角色或用户名，根据实际情况进行修改

            user_role = current_identity
            # 检查当前用户是否具有所需权限
            if enforcer.enforce(username, request.path, request.method):
                # 执行原始函数
                return func(*args, **kwargs)
            else:
                # 返回未授权的错误消息或执行其他授权失败的操作
                return "Unauthorized", 401

        return wrapper

    return decorator

# 定义受保护的路由
@app.route('/protected')
@jwt_required()
def protected():
    username = current_identity

    # 检查当前用户是否有访问权限
    if enforcer.enforce(username, request.path, request.method):
        return {'message': '授权成功'}
    else:
        return {'error': '没有访问权限'}, 403



@app.route('/api/user')
@authorize()
def user():
    return 'hello,world'

if __name__ == '__main__':
    app.run(debug=True, port=5001)
