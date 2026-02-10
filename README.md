# Howto

## Option 1: Manual Docker + Local Go
```
docker run -d -p 5432:5432 -e POSTGRES_PASSWORD=password postgres
source load_env.sh
make clean && make build && make run
```

## Option 2: Docker Compose (Recommended)
```
docker-compose up -d
# To view logs:
docker-compose logs -f app
# To stop:
docker-compose down
```

## Option 3: Development with existing database
```
# If you already have postgres running:
source load_env.sh
make run
```

user: postgres password: password

# Test

## API Endpoints

### Latest Successful Requests (one per page number):
```
curl -v http://localhost:8080/saved-stations     # Latest successful station requests from DB
curl -v http://localhost:8080/saved-prices       # Latest successful price requests from DB
```

### Most Recent Successful Requests (by timestamp):
```
curl -v http://localhost:8080/recent-stations     # Most recent 10 successful station requests
curl -v http://localhost:8080/recent-stations?limit=20  # Most recent 20 successful station requests
curl -v http://localhost:8080/recent-prices       # Most recent 10 successful price requests
curl -v http://localhost:8080/recent-prices?limit=20    # Most recent 20 successful price requests
```

### Latest Page Data:
```
curl -v http://localhost:8080/latest-station-page # Most recent successful request for highest station page
curl -v http://localhost:8080/latest-price-page   # Most recent successful request for highest price page
```

### Database Statistics:
```
curl -v http://localhost:8080/db-stats            # Database request statistics by type and status
```

# Random notes

// url

POST: 
Content-Type: application/x-www-form-urlencoded 
grant_type: client_credentials&client_id=YOUR_CLIENT_ID&client_secret=YOUR_CLIENT_SECRET&scope=fuelfinder.read 
Successful response (example)
{ "access_token": "eyJhbGciOi...", "token_type": "Bearer", "expires_in": 3600 } 
Call the API with your token
Include the token in the Authorization header on each request.

GET : /v1/prices?fuel_type=unleaded 
Authorization: Bearer ACCESS_TOKEN 


POST: https://api.fuelfinder.service.gov.uk/v1/<endpoint> 
Content-Type: text/json 
	{ 
		"CustomerName": "Joe Bloggs", 
		"Address": "", 
		"etc": etc 
	} 


1: POST

https://www.fuel-finder.service.gov.uk/api/v1/oauth/generate_access_token
code: 
token:

2: GET

https://www.fuel-finder.service.gov.uk/api/v1/pfs/fuel-prices?batch-number=1
Authorization: Bearer ACCESS_TOKEN 



POST: 
Content-Type: application/x-www-form-urlencoded 
grant_type: client_credentials&client_id=YOUR_CLIENT_ID&client_secret=YOUR_CLIENT_SECRET&scope=fuelfinder.read




https://www.fuel-finder.service.gov.uk/api/v1/pfs/fuel-prices?batch-number=1



Caching
Implement appropriate caching strategies:

Station data: Cache for 1 hour (stations don't change frequently)

Stations:
https://www.fuel-finder.service.gov.uk/api/v1/pfs?batch-number=1
batch number is page 1 (1 to 500)

Price data: Cache for 15 minutes (prices change more often)

Search results: Cache for 5 minutes (balance between freshness and performance)
Use HTTP caching headers when available
Implement cache invalidation strategies

todo: currently the api key doesn't refresh after becoming invalid
todo: data structure for cached_at dates.
todo: data structure - map out the stations and prices in ram for super fast access, prune useless info.
todo: google maps? or OSM?
todo: abolity to enter your miles per gallon? Ability to pick currency? Ability to click a link and have you navigate to it on the maps of your choice?
todo: if i travel further, how much will the journey in petrol cost?
todo: historical data store, when we update a price, make sure we store what was updated and when, even if just in raw json, so we can go back through it later.
