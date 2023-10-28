#! -*- encoding: utf-8 -*-
import concurrent.futures
import json
import threading
import time
from queue import Queue

import redis
import requests

r = redis.Redis(host='localhost', port=6380, db=0)

sub = r.pubsub()

topic_handle_map = {}


def handler_test(message):
    # time.sleep(0.01)
    return message


topic_handle_map['test'] = handler_test
topic_handle_map['test1'] = handler_test
topic_handle_map['test2'] = handler_test


def fresh_subscribe():
    while True:
        topics=list(r.smembers('Topics'))
        if len(topics)>0:
            sub.subscribe(topics)
        time.sleep(1)


threading.Thread(target=fresh_subscribe).start()


print(sub.channels)

q = Queue()


def redis_subscribe():
    while True:
        if len(sub.channels)==0:
            time.sleep(0.01)
            continue
        msg = sub.get_message()
        if msg:
            # 去掉"Topic_" 前缀
            topic = msg['channel'][len('Topic_'):]
            q.put(topic)


threading.Thread(target=redis_subscribe).start()


def wrap_result(message, result):
    url = message.get('callback_url')
    if not url:
        return
    message['id'] = message.get('id')
    message['topic'] = message.get('topic')
    message['action'] = 'response'
    # message['message'] =  message.get('id')
    message['message'] = {
        'id': message.get('id'),
        'ts': time.time_ns() - result,
        'origin_ts': result,
    }
    message = json.dumps(message)
    resp = requests.post(url, data=message)
    # print(resp.text)


def consumer():
    while True:
        topic = q.get()
        message = r.rpop(topic)
        if not message:
            continue
        handler = topic_handle_map.get(topic.decode('utf-8', 'ignore'))
        if not handler:
            continue
        origin_message = json.loads(message)
        result = handler(origin_message.get('message'))
        wrap_result(origin_message, result)


# 创建线程池
thread_pool = concurrent.futures.ThreadPoolExecutor(max_workers=400)

# 启动多个线程消费队列
for _ in range(40):
    thread_pool.submit(consumer)

# 等待线程池中的所有任务完成
thread_pool.shutdown()
