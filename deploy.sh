#!/bin/bash
set -e

# Параметры скрипта
REPO=$1
GITHUB_TOKEN=$2
GITHUB_ACTOR=$3
IMAGE_TAG=$4
TELEGRAM_BOT_TOKEN=$5
TELEGRAM_ADMIN_IDS=$6
MARZBAN_TOKEN=$7
MARZBAN_API_URL=$8
ENV_TYPE=$9           # staging или production
DEPLOY_DIR=${10}      # /opt/kurut-bot-staging или /opt/kurut-bot
LOG_LEVEL=${11}       # debug или info

echo "🚀 Deploying to $ENV_TYPE..."

# Создаем рабочую директорию если не существует
sudo mkdir -p "$DEPLOY_DIR" || mkdir -p "$DEPLOY_DIR"
sudo chown $(whoami):$(whoami) "$DEPLOY_DIR" || chown $(whoami):$(whoami) "$DEPLOY_DIR"
cd "$DEPLOY_DIR"

# Скачиваем файлы с репозитория
echo "📥 Downloading files..."
curl -o docker-compose.yml https://raw.githubusercontent.com/$REPO/main/docker-compose.yml

# Скачиваем миграции
rm -rf migrations
curl -L https://github.com/$REPO/archive/main.tar.gz | tar -xz --strip=1 '*/migrations'

# Авторизуемся в GitHub Container Registry
echo "🔐 Logging in to GHCR..."
echo "$GITHUB_TOKEN" | docker login ghcr.io -u $GITHUB_ACTOR --password-stdin

# Обновляем образ
echo "📦 Pulling Docker image..."
docker pull $IMAGE_TAG

# Создаем .env файл с переданными секретами
echo "⚙️ Creating environment for $ENV_TYPE..."
cat > .env << EOF
ENV=$ENV_TYPE
TELEGRAM_BOT_TOKEN=$TELEGRAM_BOT_TOKEN
TELEGRAM_ADMIN_TELEGRAM_IDS=$TELEGRAM_ADMIN_IDS
MARZBAN_TOKEN=$MARZBAN_TOKEN
MARZBAN_API_URL=$MARZBAN_API_URL
DB_PATH=/app/data/kurut.db
LOGGER_LEVEL=$LOG_LEVEL
IMAGE_TAG=$IMAGE_TAG
EOF

# Подготовка БД: создаем директорию с правильными правами
echo "💾 Setting up database..."
mkdir -p data
# Dockerfile использует USER appuser с UID 1000
sudo chown -R 1000:1000 data 2>/dev/null || chown -R 1000:1000 data
chmod -R 755 data

# Резервное копирование базы данных
if [ -f data/kurut.db ]; then
  echo "📋 Backing up existing database..."
  cp data/kurut.db data/kurut.db.backup.$(date +%Y%m%d_%H%M%S)
fi

# Создаем SQLite файл с правильными правами
touch data/kurut.db
sudo chown 1000:1000 data/kurut.db 2>/dev/null || chown 1000:1000 data/kurut.db
chmod 666 data/kurut.db

# Выполняем миграции через отдельный контейнер
echo "🔄 Running migrations..."
docker run --rm \
  -v $(pwd)/data:/app/data:rw \
  -v $(pwd)/migrations:/app/migrations:ro \
  --user 1000:1000 \
  $IMAGE_TAG \
  goose -dir migrations sqlite3 /app/data/kurut.db up

if [ $? -ne 0 ]; then
  echo "❌ Migration failed! Checking permissions..."
  ls -la data/
  exit 1
fi

echo "✅ Migrations completed successfully"

# Обновляем и перезапускаем сервис
echo "🚀 Starting application..."
docker-compose up -d --no-deps bot

# Ждем запуска и проверяем что приложение работает
echo "⏳ Waiting for application to start..."
sleep 15

# Проверяем что контейнер работает
if ! docker-compose ps | grep kurut-bot | grep -q "Up"; then
  echo "❌ ERROR: Container is not running!"
  docker-compose logs bot
  exit 1
fi

# Проверяем логи на ошибки
if docker-compose logs --tail=20 bot | grep -i "error\|fatal\|failed"; then
  echo "❌ ERROR: Application has errors in logs!"
  docker-compose logs bot
  exit 1
fi

echo "✅ Application deployed and running successfully"
docker-compose ps
docker-compose logs --tail=10 bot

# Очистка старых образов
docker image prune -f

echo "🎉 $ENV_TYPE deployment completed!"
