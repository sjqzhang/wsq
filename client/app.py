#! -*- encoding: utf-8 -*-
from flask import Flask, request, jsonify
from flask_sqlalchemy import SQLAlchemy
from datetime import datetime, timedelta
import json

app = Flask(__name__,instance_path='/tmp')
app.config['SQLALCHEMY_DATABASE_URI'] = 'sqlite:///test.db'
db = SQLAlchemy(app)

class NocIncident(db.Model):
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

class Alert(db.Model):
    id = db.Column(db.Integer, primary_key=True)
    event_id = db.Column(db.String(255))
    event_status = db.Column(db.String(255))
    message = db.Column(db.String(255))
    raw_message = db.Column(db.JSON)
    start_time = db.Column(db.Integer)
    end_time = db.Column(db.Integer)

@app.route('/ws/alert', methods=['GET'])
def get_alerts():
    start_time = request.args.get('start_time')
    end_time = request.args.get('end_time')
    group_id = request.args.get('group_id')

    group_where = ""
    if group_id:
        group_where = f"AND (json_extract(raw_message, '$.group_ids') LIKE '[{group_id},%' OR json_extract(raw_message, '$.group_ids') LIKE '%,{group_id},%' OR json_extract(raw_message, '$.group_ids') LIKE '%,{group_id}]' OR json_extract(raw_message, '$.group_ids') LIKE '[{group_id}]')"

    if not start_time:
        today = datetime.utcnow()
        today_start = today - timedelta(hours=24)
        query = db.session.query(Alert).filter(Alert.start_time >= today_start.timestamp(), Alert.start_time <= today.timestamp(), Alert.event_status == 'firing')
        if group_where:
            query = query.filter(group_where)
        incidents = query.all()
    else:
        if not end_time:
            end_time = start_time
        query = db.session.query(Alert).filter(Alert.start_time >= start_time, Alert.start_time <= end_time, Alert.event_status == 'firing')
        if group_where:
            query = query.filter(group_where)
        incidents = query.all()

    result = []
    for incident in incidents:
        result.append({
            'id': incident.id,
            'event_id': incident.event_id,
            'event_status': incident.event_status,
            'message': incident.message,
            'raw_message': incident.raw_message,
            'start_time': incident.start_time,
            'end_time': incident.end_time
        })

    return jsonify({
        'Code': 0,
        'Data': result,
        'Msg': 'ok'
    })

@app.route('/ws/noc_incident', methods=['GET'])
def get_noc_incidents():
    start_time = request.args.get('start_time')
    end_time = request.args.get('end_time')

    if not start_time:
        today = datetime.utcnow()
        today_start = today - timedelta(hours=24)
        incidents = db.session.query(NocIncident).filter(NocIncident.start_time >= today_start.timestamp(), NocIncident.start_time < today.timestamp()).all()
    else:
        if not end_time:
            end_time = start_time
        incidents = db.session.query(NocIncident).filter(NocIncident.start_time >= start_time, NocIncident.start_time < end_time).all()

    result = []
    for incident in incidents:
        result.append({
            'id': incident.id,
            'incident_id': incident.incident_id,
            'title': incident.title,
            'start_time': incident.start_time,
            'end_time': incident.end_time,
            'duration': incident.duration,
            'escalation_time': incident.escalation_time,
            'region': json.loads(incident.region),
            'product_line': incident.product_line,
            'lvl2_team': incident.lvl2_team,
            'lvl3_team': incident.lvl3_team,
            'metric': incident.metric,
            'record': json.loads(incident.record),
            'service_cmdb_name': incident.service_cmdb_name,
            'operator': incident.operator,
            'report_url': incident.report_url,
            'group_name': incident.group_name
        })

    return jsonify({
        'Code': 0,
        'Data': result,
        'Msg': 'ok'
    })

if __name__ == '__main__':
    with app.app_context():
        db.create_all()
    app.run(port=8867)
