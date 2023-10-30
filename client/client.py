#! -*- encoding: utf-8 -*-
import time

import websocket
import uuid
import json
import random
import threading
ws=websocket.WebSocket(enable_multithread=True)

ws.connect("ws://10.53.28.62:8866/ws")
# msg={'action': 'request', 'topic': 'test','id':str(uuid.uuid4()),'message': 'hello'}
# ws.send(json.dumps(msg))


t=time.time_ns()


def write():
    i=0
    while True:
        topics=['test','test1','test2']
        topic=random.choice(topics)
        msg={'action': 'request', 'topic': topic,'id':str(uuid.uuid4()),'message': i}
        ws.send(json.dumps(msg))
        i=i+1
        # print(i)
        if i>99:
            break

        # time.sleep(1)


def read():
    j=0
    while True:
        resp=ws.recv()
        resp=json.loads(resp)
        print(resp)
        # print((time.time_ns()- resp['message']['origin_ts'])/1000000)
        j=j+1
        if j>99:
            break
        # time.sleep(0.02)

threading.Thread(target=write).start()
time.sleep(1)
threading.Thread(target=read).start()



# print((time.time_ns()-t)/1000000)

