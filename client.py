#! -*- encoding: utf-8 -*-
import time

import websocket
import uuid
import json
import random
ws=websocket.WebSocket()

ws.connect("ws://localhost:8866/ws")
msg={'action': 'request', 'topic': 'test','id':str(uuid.uuid4()),'message': 'hello'}
ws.send(json.dumps(msg))


while True:
    topics=['test','test1','test2']
    topic=random.choice(topics)
    msg={'action': 'request', 'topic': topic,'id':str(uuid.uuid4()),'message': 'hello'}
    ws.send(json.dumps(msg))
    time.sleep(1)
