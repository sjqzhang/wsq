#!/bin/bash

for i in $(seq 10 30); do

ifconfig ens3:$i 192.168.0.$i netmask 255.255.255.0

done


for i in $(seq 10 30); do


nohup docker run --rm  -v /home/jq/ws/test:/opt  sjqzhang/alpine-glibc /opt/main -ip 192.168.0.$i >/dev/null 2>&1 &
done
