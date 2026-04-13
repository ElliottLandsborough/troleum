# Troleum

[![codecov](https://codecov.io/gh/ElliottLandsborough/troleum/graph/badge.svg?token=47IRCCVJQ0)](https://codecov.io/gh/ElliottLandsborough/troleum)

Troleum is a UK fuel price comparison website that shows petrol station locations, current fuel prices, and distance-based results on an interactive map.

Live site: https://troleum.org

The application pulls station and price data from the UK government Fuel Finder service, stores cached page responses locally, and serves a lightweight frontend for browsing nearby stations by map view, distance, and fuel type.

## What It Does

- Fetches station and fuel price data from the UK Fuel Finder API
- Caches API responses locally so startup is faster and external API usage stays lower
- Shows nearby stations on a map with station details and fuel prices
- Supports filtering by fuel type and sorting by distance or selected fuel price
- Calculates route-aware distance in the frontend when a station is selected

## Stack

- Go backend
- Static HTML, CSS, and JavaScript frontend
- Docker-based local and production runtime
- OAuth client credentials flow for Fuel Finder API access

## Data Source

Troleum uses data from the UK government Fuel Finder collection:

- https://www.gov.uk/government/collections/fuel-finder

## Local Development

### Prerequisites

- Go installed locally
- Docker available locally
- A `.env` file containing the required OAuth credentials

Required environment variables:

- `OAUTH_CLIENT_ID`
- `OAUTH_CLIENT_SECRET`

### Run Locally

```bash
source load_env.sh
make rebuildrun
```

Useful commands:

- `make test` runs the app test suite
- `make logs-app` tails the app container logs
- `make stop` stops local services
- `make clean` removes local containers and build artifacts

## Testing

Run the application tests with:

```bash
make test
```

This currently runs:

```bash
go test ./app -count=1
```

## Production Deploy

Production deploys are handled through the Makefile and now run tests before packaging and shipping the image.

```bash
make deploy-to-production
```

The production image build also stamps a fresh static asset version into the HTML so CSS and JavaScript URLs are cache-busted on each deploy.

## Project Layout

- `app/` contains the Go application code
- `static/` contains the frontend assets served by the app
- `json/` contains cached API response pages used for local persistence and startup reloads
- `diagnostics/` contains one-off analysis scripts and reference files

## Notes

- The app is designed around API rate limits and local caching
- Price and station data are refreshed on different intervals because their change frequency differs
- The frontend fetches stations based on the current map bounds and selected UI state
