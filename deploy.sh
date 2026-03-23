#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
BACKEND_DIR="$ROOT_DIR/compose-ui"
FRONTEND_DIR="$ROOT_DIR/compose-ui-web"
EMBED_DIST_DIR="$BACKEND_DIR/internal/api/webui/dist"
RELEASE_DIR="$ROOT_DIR/release"

# Parse arguments
MODE="${1:-push}"
case "$MODE" in
  test)
    PUSH=false
    ;;
  push)
    PUSH=true
    ;;
  *)
    echo "Usage: $0 [test|push]"
    echo "  test  - 本地构建当前机器架构镜像，不推送"
    echo "  push  - 构建并推送多架构镜像到仓库 (默认)"
    exit 1
    ;;
esac

# Detect current machine architecture
detect_arch() {
  case "$(uname -m)" in
    x86_64)  echo "amd64" ;;
    aarch64) echo "arm64" ;;
    arm64)   echo "arm64" ;;
    *)       echo "amd64" ;;
  esac
}

if $PUSH; then
  ARCHES=("amd64" "arm64")
  IMAGE_NAME="ghcr.io/haigeek/docker-compose-ui:latest"
  echo "模式: 构建并推送多架构镜像"
else
  ARCH=$(detect_arch)
  ARCHES=("$ARCH")
  IMAGE_NAME="ghcr.io/haigeek/docker-compose-ui:latest"
  echo "模式: 本地构建 ($ARCH)"
fi

echo "[1/5] 构建前端..."
cd "$FRONTEND_DIR"
if [ -f package-lock.json ]; then
  npm ci
else
  npm install
fi
npm run build

echo "[2/5] 同步前端静态资源到后端嵌入目录..."
mkdir -p "$EMBED_DIST_DIR"
find "$EMBED_DIST_DIR" -mindepth 1 -delete
cp -R "$FRONTEND_DIR/dist/." "$EMBED_DIST_DIR/"

mkdir -p "$RELEASE_DIR"
find "$RELEASE_DIR" -mindepth 1 -delete

echo "[3/5] 构建后端多架构二进制..."
cd "$BACKEND_DIR"
for arch in "${ARCHES[@]}"; do
  out_dir="$RELEASE_DIR/compose-ui-linux-$arch"
  mkdir -p "$out_dir"

  GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$out_dir/compose-ui" ./cmd/server

  cat > "$out_dir/.env.example" <<'ENVEOF'
COMPOSE_UI_ADDR=:8227
COMPOSE_UI_REDEPLOY_TIMEOUT=120s
COMPOSE_UI_BASIC_AUTH_USER=admin
COMPOSE_UI_BASIC_AUTH_PASS=admin
ENVEOF

  cat > "$out_dir/run.sh" <<'RUNEOF'
#!/usr/bin/env bash
set -euo pipefail
if [ -f .env ]; then
  set -a
  . ./.env
  set +a
fi
./compose-ui
RUNEOF
  chmod +x "$out_dir/run.sh"

  cat > "$out_dir/start.sh" <<'STARTEOF'
#!/usr/bin/env bash
set -euo pipefail
PID_FILE="./compose-ui.pid"

if [ -f "$PID_FILE" ]; then
  pid="$(cat "$PID_FILE")"
  if [ -n "${pid:-}" ] && kill -0 "$pid" >/dev/null 2>&1; then
    echo "compose-ui is already running (pid=$pid)"
    exit 0
  fi
fi

if [ -f .env ]; then
  set -a
  . ./.env
  set +a
fi

nohup ./compose-ui > ./compose-ui.log 2>&1 &
echo $! > "$PID_FILE"
echo "compose-ui started (pid=$(cat "$PID_FILE"))"
STARTEOF
  chmod +x "$out_dir/start.sh"

  cat > "$out_dir/stop.sh" <<'STOPEOF'
#!/usr/bin/env bash
set -euo pipefail
PID_FILE="./compose-ui.pid"

if [ ! -f "$PID_FILE" ]; then
  echo "compose-ui is not running (pid file not found)"
  exit 0
fi

pid="$(cat "$PID_FILE")"
if [ -z "${pid:-}" ] || ! kill -0 "$pid" >/dev/null 2>&1; then
  echo "compose-ui is not running (stale pid file)"
  rm -f "$PID_FILE"
  exit 0
fi

kill "$pid"
for _ in $(seq 1 20); do
  if ! kill -0 "$pid" >/dev/null 2>&1; then
    rm -f "$PID_FILE"
    echo "compose-ui stopped"
    exit 0
  fi
  sleep 0.5
done

echo "force killing compose-ui (pid=$pid)"
kill -9 "$pid" >/dev/null 2>&1 || true
rm -f "$PID_FILE"
echo "compose-ui stopped"
STOPEOF
  chmod +x "$out_dir/stop.sh"

done

echo "[4/5] 打包发行文件..."
cd "$RELEASE_DIR"
for arch in "${ARCHES[@]}"; do
  tar -czf "compose-ui-linux-$arch.tar.gz" "compose-ui-linux-$arch"
done

echo "[5/5] 构建 Docker 镜像..."
cd "$ROOT_DIR"

mkdir -p dist

for arch in "${ARCHES[@]}"; do
  cp "$RELEASE_DIR/compose-ui-linux-$arch/compose-ui" dist/compose-ui

  if $PUSH; then
    docker buildx build --platform linux/"$arch" -t "$IMAGE_NAME" --push .
  else
    docker buildx build --platform linux/"$arch" -t "$IMAGE_NAME" --load .
  fi
done

rm -f dist/compose-ui

echo ""
echo "[完成] 输出目录: $RELEASE_DIR"
ls -lh "$RELEASE_DIR"/*.tar.gz
echo ""
if $PUSH; then
  echo "Docker 镜像已推送: $IMAGE_NAME (amd64+arm64)"
else
  echo "Docker 镜像已构建: $IMAGE_NAME (本地, $arch)"
fi
