#!/bin/bash

COMMAND=$1
NUMBER=$2

echo > /tmp/ms
for i in $(seq 1 $NUMBER) ; do
cat <<EOF >>/tmp/ms
---
apiVersion: v1
kind: Namespace
metadata:
  name: sleeper-$i
EOF
done
kubectl $COMMAND -f /tmp/ms