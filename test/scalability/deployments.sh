#!/bin/bash

COMMAND=$1
NAMESPACE=$2
NUMBER=$3
REPLICAS=$4

echo > /tmp/ms
for i in $(seq 1 $NUMBER) ; do
cat <<EOF >>/tmp/ms
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: sleeper-$i
  namespace: $NAMESPACE
spec:
  replicas: $REPLICAS
  selector:
    matchLabels:
      app: sleeper-$i
  template:
    metadata:
      labels:
        app: sleeper-$i
    spec:
      terminationGracePeriodSeconds: 1
      containers:
        - name: sleeper
          image: alpine
          resources:
            requests:
              cpu: 2m
              memory: 10Mi
            limits:
              cpu: 2m
              memory: 10Mi
          command: ["/bin/sh"]
          args: ["-c", "while true; do timeout 0.5s yes >/dev/null; sleep 0.5s; done"]
---
apiVersion: autoscaling/v1
kind: HorizontalPodAutoscaler
metadata:
  name: sleeper-$i
  namespace: $NAMESPACE
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: sleeper-$i
  minReplicas: $REPLICAS
  maxReplicas: $REPLICAS
  targetCPUUtilizationPercentage: 50
EOF
done
kubectl $COMMAND -f /tmp/ms