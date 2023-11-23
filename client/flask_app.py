#! -*- encoding: utf-8 -*-
from flask_cors import CORS
from flask import Flask, Blueprint,redirect, url_for

# from flask_casbin import CasbinManager



# 创建蓝图
blueprint = Blueprint('/v2', __name__)
app = Flask(__name__,instance_path='/tmp/flask_app')

from casbin import Enforcer

# 配置 Casbin 模型和策略
# casbin_model_path = 'model.conf'
# casbin_policy_path = 'policy.csv'
# enforcer = Enforcer(casbin_model_path, casbin_policy_path)
#
# # 初始化 Flask-Casbin 扩展
# casbin = CasbinManager(app)
# 配置 Flask-Casbin 扩展
# app.config['CASBIN_ENFORCER'] = enforcer


CORS(app, supports_credentials=True)

@blueprint.route('/guest')
def index():
    return 'hello guest'

# 404页面
@app.errorhandler(404)
def page_not_found(e):
    return 'not found'

# 将蓝图注册到app
app.register_blueprint(blueprint, url_prefix='/v2')
if __name__ == '__main__':
    app.run(host='0.0.0.0', port=5001, debug=True)

