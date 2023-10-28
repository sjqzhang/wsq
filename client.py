#! -*- encoding: utf-8 -*-
import time

import websocket
import uuid
import json
import random
import threading
ws=websocket.WebSocket(enable_multithread=True)

ws.connect("ws://192.168.1.12:8866/ws")
# msg={'action': 'request', 'topic': 'test','id':str(uuid.uuid4()),'message': 'hello'}
# ws.send(json.dumps(msg))

i=0
def write():

    while True:
        global i
        topics=['test','test1','test2']
        topic=random.choice(topics)
        msg={'action': 'request', 'topic': topic,'id':str(uuid.uuid4()),'message': time.time_ns()}
        ws.send(json.dumps(msg))
        i=i+1
        # print(i)
        # if i>10:
        #     break

        # time.sleep(1)


def read():
    while True:
        resp=ws.recv()
        resp=json.loads(resp)
        print((time.time_ns()- resp['message']['origin_ts'])/1000000)
        # time.sleep(0.02)

threading.Thread(target=write).start()
threading.Thread(target=read).start()

