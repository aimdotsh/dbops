#!/usr/bin/env bash
set -euo pipefail
# No network, published ports, host data directories or existing database volumes.
root=$(cd "$(dirname "$0")/.." && pwd)
artifacts=$(mktemp -d)
name="dbops-real-mysql-${RANDOM}-${RANDOM}"
cleanup() { docker rm -fv "$name" >/dev/null 2>&1 || true; rm -rf "$artifacts"; }
trap cleanup EXIT
cd "$root"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c -tags integration -o "$artifacts/agent.test" ./internal/agentclient
docker run -d --platform linux/amd64 --name "$name" --network none \
 -e MYSQL_ROOT_PASSWORD=Dbops-disposable-test-123 \
 -e DBOPS_DISPOSABLE_MYSQL=1 \
 -v "$artifacts:/test:ro" mysql:8.0.46 --socket=/var/run/mysqld/mysql.sock >/dev/null
ready=false
for i in $(seq 1 90); do
 if docker exec "$name" mysql --socket=/var/run/mysqld/mysql.sock --user=root --password=Dbops-disposable-test-123 --batch --skip-column-names -e "SELECT @@GLOBAL.skip_networking" 2>/dev/null | grep -qx 0; then ready=true; break; fi
 sleep 1
done
if [ "$ready" != true ]; then docker logs "$name"; exit 1; fi
docker exec "$name" /test/agent.test -test.v -test.run '^TestDisposableMySQL'
