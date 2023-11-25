#! -*- encoding: utf-8 -*-
from flask import Flask, request,g,jsonify
from casbin import Enforcer
from flask_jwt import JWT, jwt_required, current_identity
from werkzeug.security import safe_str_cmp
from functools import wraps
from datetime import datetime, timedelta
from flask import Blueprint

blueprint = Blueprint('ws',__name__,url_prefix='/ws',)
app = Flask(__name__,instance_path='/tmp')

app.config['SECRET_KEY'] = 'supersecretkey'
app.config['JWT_EXPIRATION_DELTA']=timedelta(days=365*10)

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

# 统一的返回处理装饰器
def response_handler(f):
    def wrapper(*args, **kwargs):
        try:
            # 执行路由处理函数
            response = f(*args, **kwargs)
            # 返回JSON响应
            return jsonify({'data': response, 'error': None}), 200
        except Exception as e:
            # 处理异常情况
            return jsonify({'data': None, 'error': str(e)}), 500
    return wrapper

def authorize():

    def decorator(func):
        @jwt_required()
        @wraps(func)
        def wrapper(*args, **kwargs):
            # 获取当前用户的角色或用户名，根据实际情况进行修改

            username = current_identity
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
@authorize()
def protected():
    return "This is a protected route"

@app.route('/api/user')
@authorize()
def api_user():
    return "This is an API route"

#blueprint example

@blueprint.route('/')
def index():
    return 'index'


@blueprint.route('/protected')
@authorize()
def blueprint_protected():
    return 'protected'

if __name__ == '__main__':
    app.register_blueprint(blueprint)
    app.run(debug=True, port=5001)
