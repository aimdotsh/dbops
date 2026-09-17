#!/usr/bin/env bash
set -euo pipefail
: "${DBOPS_MASTER_KEY:?export DBOPS_MASTER_KEY first}"
mkdir -p /data/dbops
docker run -d \
  --name dbops \
  --restart unless-stopped \
  -p 8080:8080 \
  -v /data/dbops:/data/dbops \
  -e DBOPS_MASTER_KEY \
  dbops/dbops-server:1.0.0
