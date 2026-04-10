let map;
let fuelTypes = [];
let fuelTypesPromise = null;
let latestPins = null;
let markersById = new Map();
let infoWindowsById = new Map();
let debounceTimer = null;
let abortController = null;
let userLat = null;
let userLng = null;
const USER_MARKER_Z_INDEX = 1000000;
const FUEL_TYPE_DISPLAY_ORDER = [
    'E10',
    'E5',
    'B7_STANDARD',
    'B7_PREMIUM',
    'HVO',
    'B10',
];
let isFollowingMyLocation = true;
let pendingFollowMeLocationRequest = false;
let selectedStationMarkerId = null;

function updateFollowMeUI() {
    const btn = document.getElementById('my-location');
    const input = document.getElementById('location-input');
    const toggleBtn = document.getElementById('search-mode-toggle');

    if (isFollowingMyLocation) {
        btn.style.opacity = '1';
        btn.style.cursor = 'pointer';
        input.disabled = true;
        input.style.opacity = '0.5';
        toggleBtn.style.opacity = '0.5';
        toggleBtn.style.cursor = 'not-allowed';
        toggleBtn.disabled = false;
    } else {
        btn.style.opacity = '1';
        btn.style.cursor = 'pointer';
        input.disabled = false;
        input.style.opacity = '1';
        toggleBtn.style.opacity = '0.5';
        toggleBtn.style.cursor = 'not-allowed';
        toggleBtn.disabled = true;
    }
}

function populateFollowMeLocationInput(lat, lng) {
    const input = document.getElementById('location-input');
    const geocoder = new google.maps.Geocoder();

    geocoder.geocode({ location: { lat, lng } }, (results, status) => {
        if (isFollowingMyLocation && status === 'OK' && results[0]) {
            input.value = results[0].formatted_address;
        } else if (status !== 'OK') {
            console.warn('Reverse geocoding failed:', status);
        }

        input.placeholder = 'Enter a location';
    });
}

function applyPendingFollowMeLocation(lat, lng) {
    if (!pendingFollowMeLocationRequest || !isFollowingMyLocation) {
        return;
    }

    pendingFollowMeLocationRequest = false;
    map.setCenter({ lat, lng });
    map.setZoom(14);
    populateFollowMeLocationInput(lat, lng);
}

function setFollowMeMode() {
    isFollowingMyLocation = true;
    document.getElementById('location-input').value = '';
    document.getElementById('location-input').placeholder = 'Enter a location';

    markersById.forEach((marker, id) => {
        if (id === 'search-location') {
            marker.setVisible(false);
        } else if (id === 'user-location') {
            marker.setVisible(true);
        }
    });

    updateFollowMeUI();
}

function setSearchLocationMode(lat, lng) {
    isFollowingMyLocation = false;

    markersById.forEach((marker, id) => {
        if (id === 'user-location') {
            marker.setVisible(false);
        } else if (id === 'search-location') {
            marker.setVisible(true);
        }
    });

    updateFollowMeUI();
}

function toggleSearchMode() {
    if (isFollowingMyLocation) {
        // Switch to search mode (blue dot hidden, input enabled)
        isFollowingMyLocation = false;
        markersById.forEach((marker, id) => {
            if (id === 'user-location') {
                marker.setVisible(false);
            }
        });
        updateFollowMeUI();
        document.getElementById('location-input').placeholder = 'Enter a location';
        document.getElementById('location-input').focus();
    } else {
        // Switch back to follow me mode
        setFollowMeMode();
    }
}

function preloadFuelTypes() {
    if (fuelTypesPromise) {
        return fuelTypesPromise;
    }

    const fuelTypesUrl = new URL('/api/fuel-types', window.location.origin);

    fuelTypesPromise = fetch(fuelTypesUrl)
        .then(res => res.json())
        .then(payload => {
            fuelTypes = Array.isArray(payload?.data) ? payload.data : [];
            fuelTypes.sort(compareFuelTypes);
            return fuelTypes;
        })
        .catch(err => {
            console.error('Failed to preload fuel types:', err);
            fuelTypes = [];
            return fuelTypes;
        });

    return fuelTypesPromise;
}

function getFuelTypeLabel(fuelType) {
    const labels = {
        E10: 'Petrol (E10)',
        E5: 'Premium Petrol (E5)',
        B7_STANDARD: 'Diesel (B7)',
        B7_PREMIUM: 'Premium Diesel (B7)',
        HVO: 'Renewable Diesel (HVO)',
        B10: 'Diesel (B10)',
    };

    return labels[fuelType] || fuelType;
}

function compareFuelTypes(left, right) {
    const leftIndex = FUEL_TYPE_DISPLAY_ORDER.indexOf(left);
    const rightIndex = FUEL_TYPE_DISPLAY_ORDER.indexOf(right);

    if (leftIndex === -1 && rightIndex === -1) {
        return String(left).localeCompare(String(right));
    }

    if (leftIndex === -1) {
        return 1;
    }

    if (rightIndex === -1) {
        return -1;
    }

    return leftIndex - rightIndex;
}

function getSortedPrices(prices) {
    if (!Array.isArray(prices)) {
        return [];
    }

    return [...prices].sort((left, right) => compareFuelTypes(left.fuel_type, right.fuel_type));
}

function getFuelPriceForType(pin, fuelType) {
    if (!Array.isArray(pin?.prices)) {
        return null;
    }

    const matchingPrice = pin.prices.find(price => price?.fuel_type === fuelType);
    if (!matchingPrice) {
        return null;
    }

    const numericPrice = Number(matchingPrice.price);
    return Number.isFinite(numericPrice) ? numericPrice : null;
}

function getSortedPinsForSelection(pinList) {
    const sortValue = document.getElementById('sort-options')?.value || 'distance';
    if (sortValue === 'distance') {
        return pinList;
    }

    return [...pinList].sort((left, right) => {
        const leftPrice = getFuelPriceForType(left, sortValue);
        const rightPrice = getFuelPriceForType(right, sortValue);

        if (leftPrice == null && rightPrice == null) {
            return String(left.name || '').localeCompare(String(right.name || ''));
        }

        if (leftPrice == null) {
            return 1;
        }

        if (rightPrice == null) {
            return -1;
        }

        return leftPrice - rightPrice;
    });
}

function getSelectedFuelSortValue() {
    const selectedValue = document.getElementById('sort-options')?.value || 'distance';
    return selectedValue === 'distance' ? null : selectedValue;
}

function getFuelTypeLabelHtml(fuelType, selectedFuelType) {
    const label = escapeHtml(getFuelTypeLabel(fuelType));
    if (selectedFuelType && selectedFuelType === fuelType) {
        return `<strong>${label}</strong>`;
    }

    return label;
}

function getFuelPriceHtml(fuelType, price, selectedFuelType) {
    const safePrice = escapeHtml(String(price));
    if (selectedFuelType && selectedFuelType === fuelType) {
        return `<strong>${safePrice}</strong>`;
    }

    return safePrice;
}

function getDistanceHtml(distance, isDistanceSelected) {
    const valueText = distance != null ? `${distance.toFixed(2)} mile` : 'N/A';
    const safeValueText = escapeHtml(valueText);

    if (isDistanceSelected) {
        return `<strong><span class="title">📏 Distance: </span>${safeValueText}</strong>`;
    }

    return `<span class="title">📏 Distance: </span>${safeValueText}`;
}

function updateSortOptionsFromPins(pinList) {
    const sortSelect = document.getElementById('sort-options');
    const previousValue = sortSelect.value || 'distance';

    const uniqueFuelTypes = new Set();
    pinList.forEach(pin => {
        if (!Array.isArray(pin.prices)) {
            return;
        }

        pin.prices.forEach(price => {
            if (price?.fuel_type) {
                uniqueFuelTypes.add(price.fuel_type);
            }
        });
    });

    const sortedFuelTypes = [...uniqueFuelTypes].sort(compareFuelTypes);

    sortSelect.innerHTML = '';

    const distanceOption = document.createElement('option');
    distanceOption.value = 'distance';
    distanceOption.textContent = 'Distance (order by)';
    sortSelect.appendChild(distanceOption);

    sortedFuelTypes.forEach(fuelType => {
        const option = document.createElement('option');
        option.value = fuelType;
        option.textContent = getFuelTypeLabel(fuelType);
        sortSelect.appendChild(option);
    });

    const hasPreviousOption = previousValue === 'distance' || uniqueFuelTypes.has(previousValue);
    sortSelect.value = hasPreviousOption ? previousValue : 'distance';
}

function escapeHtml(text) {
    return String(text)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');
}

function getMapsUrl(lat, lng, label) {
    const isIOS = /iPhone|iPad|iPod/i.test(navigator.userAgent);
    const isAndroid = /Android/i.test(navigator.userAgent);
    const encodedLabel = encodeURIComponent(label || `${lat},${lng}`);

    if (isIOS) {
        return `https://maps.apple.com/?ll=${lat},${lng}&q=${encodedLabel}`;
    }

    if (isAndroid) {
        return `geo:${lat},${lng}?q=${lat},${lng}(${encodedLabel})`;
    }

    return `https://www.google.com/maps/search/?api=1&query=${lat},${lng}`;
}

function buildAddressLinkHtml(pin) {
    const addressText = pin.address || 'No address available';
    if (pin.lat == null || pin.lng == null) {
        return escapeHtml(addressText);
    }

    const mapsUrl = getMapsUrl(pin.lat, pin.lng, pin.name || addressText);
    return `<a href="${mapsUrl}" target="_blank" rel="noopener noreferrer">${escapeHtml(addressText)}</a>`;
}

function updateSelectedLocationListItem(markerId) {
    selectedStationMarkerId = markerId == null ? null : String(markerId);

    document.querySelectorAll('#location-list .location-list-item').forEach(item => {
        item.classList.toggle('is-selected', item.dataset.markerId === selectedStationMarkerId);
    });
}

function openStationInfoWindow(markerId) {
    const normalizedMarkerId = String(markerId);
    const marker = markersById.get(normalizedMarkerId);
    const infoWindow = infoWindowsById.get(normalizedMarkerId);

    if (!marker || !infoWindow) {
        return;
    }

    infoWindowsById.forEach(iw => iw.close());
    updateSelectedLocationListItem(normalizedMarkerId);
    map.panTo(marker.getPosition());
    infoWindow.open(map, marker);
}

function initMap() {
    // Create map centered on the world
    map = new google.maps.Map(document.getElementById('map'), {
        zoom: 6,
        center: { lat: 54.23782, lng: -4.555111 },
        mapTypeControl: true,
        streetViewControl: false
    });

    // Constantly update a blue dot on the map showing the user's current location, and center the map on it when the page loads
    if (navigator.geolocation) {
        navigator.geolocation.watchPosition((position) => {
            const lat = position.coords.latitude;
            const lon = position.coords.longitude;

            // Cache latest coordinates for station API requests.
            userLat = lat;
            userLng = lon;

            // Create or update a marker for the user's location
            if (!markersById.has('user-location')) {
                const userMarker = new google.maps.Marker({
                    position: { lat, lng: lon },
                    map: map,
                    title: 'Your Location',
                    zIndex: USER_MARKER_Z_INDEX,
                    icon: {
                        path: google.maps.SymbolPath.CIRCLE,
                        scale: 8,
                        fillColor: '#4285F4',
                        fillOpacity: 1,
                        strokeColor: '#ffffff',
                        strokeWeight: 2,
                    },
                });
                markersById.set('user-location', userMarker);
            } else {
                const userMarker = markersById.get('user-location');
                userMarker.setPosition({ lat, lng: lon });
                userMarker.setZIndex(USER_MARKER_Z_INDEX);
                if (isFollowingMyLocation) {
                    userMarker.setVisible(true);
                }
            }

            applyPendingFollowMeLocation(lat, lon);
        });
    }

    // Fire-and-forget preload: starts immediately but never blocks map/markers rendering.
    preloadFuelTypes().then(() => {
        if (latestPins) {
            renderStationInfo(latestPins);
        }
    });

    // Create bounds to fit all markers
    //const bounds = new google.maps.LatLngBounds();

    map.addListener('idle', () => {
        clearTimeout(debounceTimer);

        const url = new URL('/api/stations', window.location.origin);
        // Use last known user location from watchPosition if available.
        if (userLat != null && userLng != null) {
            url.searchParams.set('lat', userLat);
            url.searchParams.set('lng', userLng);
        }

        const bounds = map.getBounds();
        const ne = bounds.getNorthEast();
        const sw = bounds.getSouthWest();

        url.searchParams.set('bbox', `${sw.lat()},${sw.lng()},${ne.lat()},${ne.lng()}`);

        debounceTimer = setTimeout(async () => {
            // Cancel any previous in-flight request
            abortController?.abort();
            abortController = new AbortController();

            try {
                const res = await fetch(url, { signal: abortController.signal });
                const pins = await res.json();
                latestPins = pins;
                renderPins(pins);
                renderStationInfo(pins);
            } catch (err) {
                if (err.name === 'AbortError') return; // expected, ignore
                console.error(err);
            }
        }, 200);
    });

    const input = document.getElementById('location-input');
    const sortSelect = document.getElementById('sort-options');

    sortSelect.addEventListener('change', () => {
        if (latestPins) {
            renderPins(latestPins);
            renderStationInfo(latestPins);
        }
    });

    const autocomplete = new google.maps.places.Autocomplete(input);

    autocomplete.bindTo('bounds', map);

    autocomplete.setComponentRestrictions({ country: 'uk' });
    autocomplete.setBounds(map.getBounds());

    //const marker = new google.maps.Marker({
    //    map: map,
    //});

    autocomplete.addListener('place_changed', () => {
        const place = autocomplete.getPlace();

        if (!place.geometry) {
            alert('No details available for this location');
            return;
        }

        const lat = place.geometry.location.lat();
        const lng = place.geometry.location.lng();

        if (!markersById.has('search-location')) {
            const searchMarker = new google.maps.Marker({
                position: { lat, lng },
                map: map,
                title: 'Search Location',
                zIndex: 999999,
                icon: {
                    path: google.maps.SymbolPath.CIRCLE,
                    scale: 8,
                    fillColor: '#34A853',
                    fillOpacity: 1,
                    strokeColor: '#ffffff',
                    strokeWeight: 2,
                },
            });
            markersById.set('search-location', searchMarker);
        } else {
            const searchMarker = markersById.get('search-location');
            searchMarker.setPosition({ lat, lng });
            searchMarker.setVisible(true);
        }

        setSearchLocationMode(lat, lng);

        // Center map on the selected marker itself.
        map.setCenter({ lat, lng });
        map.setZoom(14);
    });

    centerMapOnUserLocation();
}

function renderPins(pins) {
    const pinList = Array.isArray(pins?.data) ? pins.data : [];
    console.warn(`Fetched ${pinList.length} pins from the server`);
    const selectedFuelType = getSelectedFuelSortValue();
    const isDistanceSelected = selectedFuelType == null;

    updateSortOptionsFromPins(pinList);

    const nextIds = new Set(pinList.map(pin => String(pin.id)));

    // Remove markers that are no longer in the latest results.
    markersById.forEach((marker, id) => {
        if (id === 'user-location') return; // never remove the user location marker
        if (id === 'search-location') return; // never remove the search location marker
        if (!nextIds.has(id)) {
            marker.setMap(null);
            markersById.delete(id);

            if (selectedStationMarkerId === id) {
                selectedStationMarkerId = null;
            }

            const infoWindow = infoWindowsById.get(id);
            if (infoWindow) {
                infoWindow.close();
                infoWindowsById.delete(id);
            }
        }
    });

    const bounds = new google.maps.LatLngBounds();

    pinList.forEach(pin => {
        const id = String(pin.id);
        const sortedPrices = getSortedPrices(pin.prices);
        if (id === 'user-location') return; // never overwrite the user location marker
        const infoContent = `
            <div class="info-window">
                <h3>${pin.name}</h3><br />
                <p class="distance">${getDistanceHtml(pin.distance, isDistanceSelected)}</p><br />
                <div class="prices-header">⛽ Prices:</div>
                <table class="prices"><thead><tr><th>Fuel type</th><th>Price</th></tr></thead><tbody>${sortedPrices.length > 0 ? sortedPrices.map(p => `<tr><td>${getFuelTypeLabelHtml(p.fuel_type, selectedFuelType)}</td><td>${getFuelPriceHtml(p.fuel_type, p.price, selectedFuelType)}</td></tr>`).join('') : '<tr><td colspan="2">No price data available</td></tr>'}</tbody></table><br />
                <p class="address">📍 Address:<br />${buildAddressLinkHtml(pin)}</p><br />
                <p class="phone">📞 Telephone:<br />${pin.phone ? `<a href="tel:${pin.phone}">${pin.phone}</a>` : 'No phone available'}</p>
            </div>
        `;

        if (markersById.has(id)) {
            // Keep existing marker and update mutable fields.
            const existingMarker = markersById.get(id);
            existingMarker.setPosition({ lat: pin.lat, lng: pin.lng });
            existingMarker.setTitle(pin.name || '');

            const existingInfoWindow = infoWindowsById.get(id);
            if (existingInfoWindow) {
                existingInfoWindow.setContent(infoContent);
            }

            bounds.extend(existingMarker.getPosition());
            return;
        }

        const marker = new google.maps.Marker({
            position: { lat: pin.lat, lng: pin.lng },
            map: map,
            title: pin.name,
            //animation: google.maps.Animation.DROP
        });

        const infoWindow = new google.maps.InfoWindow({
            content: infoContent
        });

        infoWindow.addListener('closeclick', function() {
            updateSelectedLocationListItem(null);
        });

        marker.addListener('click', function() {
            openStationInfoWindow(id);
        });

        bounds.extend(marker.getPosition());

        markersById.set(id, marker);
        infoWindowsById.set(id, infoWindow);
    });

    // Fit map to show all markers
    //map.fitBounds(bounds);
}

// Create a table of station information in the info div on the right side of the screen, based on the latest pins data from the server
function renderStationInfo(pins) {
    const infoDiv = document.getElementById('location-list');
    const pinList = Array.isArray(pins?.data) ? pins.data : [];
    const sortedPinList = getSortedPinsForSelection(pinList);
    const selectedFuelType = getSelectedFuelSortValue();
    const isDistanceSelected = selectedFuelType == null;

    if (sortedPinList.length === 0) {
        infoDiv.innerHTML = '<p>No stations found in this area.</p>';
        return;
    }

    const stationInfoHtml = sortedPinList.map(pin => {
        const sortedPrices = getSortedPrices(pin.prices);

        return `
        <div class="location-list-item" data-marker-id="${escapeHtml(String(pin.id))}">
            <h3>${pin.name}</h3>
            <p class="distance">${getDistanceHtml(pin.distance, isDistanceSelected)}</p>
            <table class="prices"><thead><tr><th>Fuel type</th><th>Price</th></tr></thead><tbody>${sortedPrices.length > 0 ? sortedPrices.map(p => `<tr><td class="price-label">${getFuelTypeLabelHtml(p.fuel_type, selectedFuelType)}</td><td class="price-value">${getFuelPriceHtml(p.fuel_type, p.price, selectedFuelType)}</td></tr>`).join('') : '<tr><td colspan="2">No price data available</td></tr>'}</tbody></table>
        </div>
    `;
    }).join('<hr>');

    infoDiv.innerHTML = stationInfoHtml;

    infoDiv.querySelectorAll('.location-list-item').forEach(item => {
        item.addEventListener('click', () => {
            openStationInfoWindow(item.dataset.markerId);
        });
    });

    updateSelectedLocationListItem(selectedStationMarkerId);
}

// Function to get the user's current location and center the map on it
function centerMapOnUserLocation() {
    setFollowMeMode();
    pendingFollowMeLocationRequest = true;

    if (navigator.geolocation) {
        console.warn('Attempting to get user location via Geolocation API...');
        document.getElementById('location-input').placeholder = 'Searching for your location, please wait';
        if (userLat !== null && userLng !== null) {
            applyPendingFollowMeLocation(userLat, userLng);
        }
    } else {
        console.warn('Geolocation is not supported by this browser, cannot center map on user location');
        document.getElementById('location-input').placeholder = 'Enter a location';
    }
}

document.addEventListener('DOMContentLoaded', function() {
    updateFollowMeUI();
});
