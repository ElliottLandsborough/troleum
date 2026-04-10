# Howto

## Option 1: Manual Docker + Local Go
```
source load_env.sh
make rebuildrun
```

## Option 2: Docker Compose (Recommended)
```
docker-compose up -d
# To view logs:
docker-compose logs -f app
# To stop:
docker-compose down
```
