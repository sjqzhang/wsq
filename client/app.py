#! -*- encoding: utf-8 -*-
from flask import Flask, request, jsonify
from flask_sqlalchemy import SQLAlchemy
import json
import time
import requests


app = Flask(__name__,instance_path='/tmp')
app.config['SQLALCHEMY_DATABASE_URI'] = 'sqlite:///test.db'

import logging
logging.basicConfig()
logging.getLogger('sqlalchemy.engine').setLevel(logging.INFO)
app.config['SQLALCHEMY_ECHO'] = True
app.config['SQLALCHEMY_COMMIT_ON_TEARDOWN'] = True
db = SQLAlchemy(app)

class Subscription:
    def __init__(self, action, topic, id, message, header):
        self.action = action
        self.topic = topic
        self.id = id
        self.message = message
        self.header = header

class NocIncident(db.Model):
    __tablename__ = 'noc_incidents'
    id = db.Column(db.Integer, primary_key=True)
    incident_id = db.Column(db.String(255))
    title = db.Column(db.String(255))
    start_time = db.Column(db.Integer)
    end_time = db.Column(db.Integer)
    duration = db.Column(db.Integer)
    escalation_time = db.Column(db.Integer)
    region = db.Column(db.JSON)
    product_line = db.Column(db.String(255))
    lvl2_team = db.Column(db.String(255))
    lvl3_team = db.Column(db.String(255))
    metric = db.Column(db.String(255))
    record = db.Column(db.JSON)
    service_cmdb_name = db.Column(db.String(255))
    operator = db.Column(db.String(255))
    report_url = db.Column(db.String(255))
    group_name = db.Column(db.String(255))
    def serialize(self):
        return {
            'id': self.id,
            'incident_id': self.incident_id,
            'title': self.title,
            'start_time': self.start_time,
            'end_time': self.end_time,
            'duration': self.duration,
            'escalation_time': self.escalation_time,
            'region': self.region,
            'product_line': self.product_line,
            'lvl2_team': self.lvl2_team,
            'lvl3_team': self.lvl3_team,
            'metric': self.metric,
            'record': self.record,
            'service_cmdb_name': self.service_cmdb_name,
            'operator': self.operator,
            'report_url': self.report_url,
            'group_name': self.group_name
        }


class Alert(db.Model):
    __tablename__ = 'alerts'
    id = db.Column(db.Integer, primary_key=True)
    event_id = db.Column(db.String(255))
    event_status = db.Column(db.String(255))
    message = db.Column(db.String(255))
    raw_message = db.Column(db.JSON)
    start_time = db.Column(db.Integer)
    end_time = db.Column(db.Integer)
    def serialize(self):
        return {
            'id': self.id,
            'event_id': self.event_id,
            'event_status': self.event_status,
            'message': self.message,
            'raw_message': self.raw_message,
            'start_time': self.start_time,
            'end_time': self.end_time
        }


@app.route('/')
def home():
    with open('examples/message/home.html', 'r') as file:
        return file.read()


@app.route('/ws/alert', methods=['GET'])
def get_alerts():
    start_time = request.args.get('start_time')
    end_time = request.args.get('end_time')
    group_id = request.args.get('group_id')

    group_where = ""
    if group_id:
        group_where = f"AND (json_extract(raw_message, '$.group_ids') LIKE '[{group_id},%' " \
                      f"OR json_extract(raw_message, '$.group_ids') LIKE '%,{group_id},%' " \
                      f"OR json_extract(raw_message, '$.group_ids') LIKE '%,{group_id}]' " \
                      f"OR json_extract(raw_message, '$.group_ids') LIKE '[{group_id}]')"

    if not start_time:
        today = int(time.time())
        today_start = today - (24 * 60 * 60)
        query = Alert.query.filter(Alert.start_time >= today_start, Alert.start_time <= today)
        if group_where:
            query = query.filter(Alert.event_status == 'firing').filter(group_where)
        else:
            query = query.filter(Alert.event_status == 'firing')
        incidents = query.all()
    else:
        if not end_time:
            end_time = start_time
        query = Alert.query.filter(Alert.start_time >= start_time, Alert.start_time <= end_time)
        if group_where:
            query = query.filter(Alert.event_status == 'firing').filter(group_where)
        else:
            query = query.filter(Alert.event_status == 'firing')
        incidents = query.all()

    return jsonify({'retcode': 0, 'data': [incident.serialize() for incident in incidents], 'message': 'ok'})


@app.route('/ws/noc_incident', methods=['GET'])
def get_noc_incidents():
    start_time = request.args.get('start_time')
    end_time = request.args.get('end_time')

    if not start_time:
        today = int(time.time())
        today_start = today - (24 * 60 * 60)
        incidents = NocIncident.query.filter(NocIncident.start_time >= today_start, NocIncident.start_time < today).all()
    else:
        if not end_time:
            end_time = start_time
        incidents = NocIncident.query.filter(NocIncident.start_time >= start_time, NocIncident.start_time < end_time).all()

    return jsonify({'retcode': 0, 'data': [incident.serialize() for incident in incidents], 'message': 'ok'})


@app.route('/ws/alert', methods=['POST'])
def create_alert():
    data = request.get_json()
    incident = Alert.query.filter_by(event_id=data['event_id']).first()
    if incident is None:
        incident = Alert(event_id=data['event_id'], event_status=data['event_status'], message=data['message'],
                         raw_message=data['raw_message'], start_time=data['start_time'], end_time=data['end_time'])
        db.session.add(incident)
    else:
        incident.event_status = data['event_status']
        incident.message = data['message']
        incident.raw_message = data['raw_message']
        incident.start_time = data['start_time']
        incident.end_time = data['end_time']
    db.session.commit()

    requests.post('http://127.0.0.1:8866/ws/api', json=Subscription(topic='alert',message=data).__dict__)

    return jsonify({'retcode': 0, 'data': data, 'message': 'ok'})


@app.route('/ws/noc_incident', methods=['POST'])
def create_noc_incident():
    data = request.get_json()
    incident = NocIncident.query.filter_by(incident_id=data['incident_id']).first()
    if incident is None:
        incident = NocIncident(incident_id=data['incident_id'], title=data['title'], start_time=data['start_time'],
                               end_time=data['end_time'], duration=data['duration'],
                               escalation_time=data['escalation_time'], region=data['region'],
                               product_line=data['product_line'], lvl2_team=data['lvl2_team'],
                               lvl3_team=data['lvl3_team'], metric=data['metric'], record=data['record'],
                               service_cmdb_name=data['service_cmdb_name'], operator=data['operator'],
                               report_url=data['report_url'], group_name=data['group_name'])
        db.session.add(incident)
    else:
        incident.title = data['title']
        incident.start_time = data['start_time']
        incident.end_time = data['end_time']
        incident.duration = data['duration']
        incident.escalation_time = data['escalation_time']
        incident.region = data['region']
        incident.product_line = data['product_line']
        incident.lvl2_team = data['lvl2_team']
        incident.lvl3_team = data['lvl3_team']
        incident.metric = data['metric']
        incident.record = data['record']
        incident.service_cmdb_name = data['service_cmdb_name']
        incident.operator = data['operator']
        incident.report_url = data['report_url']
        incident.group_name = data['group_name']
    db.session.commit()


    requests.post('http://127.0.0.1:8866/ws/api', json=Subscription(topic='noc_incident',message=data).__dict__)

    return jsonify({'retcode': 0, 'data': data, 'message': 'ok'})


if __name__ == '__main__':
    # with app.app_context():
    #     db.create_all()
    app.run(host='0.0.0.0', port=8867,debug=True)
