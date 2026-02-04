# Howto

```
source load_env.sh
make run
make clean
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

todo: data structure for cached_at dates.
todo: data structure - map out the stations and prices in ram for super fast access, prune useless info.
todo: google maps? or OSM?
todo: abolity to enter your miles per gallon? Ability to pick currency? Ability to click a link and have you navigate to it on the maps of your choice?
todo: if i travel further, how much will the journey in petrol cost?
todo: historical data store, when we update a price, make sure we store what was updated and when, even if just in raw json, so we can go back through it later.
