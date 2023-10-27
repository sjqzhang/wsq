#! -*- encoding: utf-8 -*-
import threading
from queue import Queue
import concurrent.futures

import redis

r = redis.Redis(host='localhost', port=6379, db=0)


sub = r.pubsub()


topic_handle_map={}



def handler_test(message):
    print(message)


topic_handle_map['test']=handler_test


sub.subscribe(list(r.smembers('Topics')))

print(sub.channels)

q = Queue()


def redis_subscribe():
    while True:
        msg = sub.get_message()
        if msg:
            #去掉"Topic_" 前缀
            topic = msg['channel'][len('Topic_'):]
            q.put(topic)


threading.Thread(target=redis_subscribe).start()


def wrap_result(result):


def consumer():
    while True:
        topic = q.get()
        message=r.rpop(topic)
        result=topic_handle_map[topic](message)



# 创建线程池
thread_pool = concurrent.futures.ThreadPoolExecutor(max_workers=4)

# 启动多个线程消费队列
for _ in range(4):
    thread_pool.submit(consumer)

# 等待线程池中的所有任务完成
thread_pool.shutdown()


