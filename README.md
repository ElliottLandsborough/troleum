# troleum

[![codecov](https://codecov.io/gh/ElliottLandsborough/troleum/graph/badge.svg?token=47IRCCVJQ0)](https://codecov.io/gh/ElliottLandsborough/troleum)

## Info

Website: https://troleum.org

Gets petrol station locations and prices from https://www.gov.uk/government/collections/fuel-finder and presents them in a relatively easy to use interface

## Howto

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
