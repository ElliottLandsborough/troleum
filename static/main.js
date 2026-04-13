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
let routesApiRouteClass = null;
let activeRoutePolylines = [];
let routeDistanceMilesByStationId = new Map();
let userLocationInfoWindow = null;
let userEstimatedAddress = null;
let searchLocationInfoWindow = null;
let searchSetAddress = null;
let placesAutocomplete = null;
let locationInputElement = null;
let requestStationsForCurrentView = null;
let activeRouteStationId = null;
const USER_MARKER_Z_INDEX = 1000000;
const MARKER_COLOR_DEFAULT = '#4285F4';
const MARKER_COLOR_CHEAPEST = '#34A853';
const MARKER_COLOR_MEDIUM = '#FBBC05';
const MARKER_COLOR_EXPENSIVE = '#EA4335';
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
const GOOGLE_MAPS_MAP_ID = '570b6285826fd5d96eb33627';
const MAP_THEME_MEDIA_QUERY = '(prefers-color-scheme: dark)';
const UK_AUTOCOMPLETE_BOUNDS = {
    north: 61.2,
    south: 49.8,
    west: -8.8,
    east: 2.1,
};
const GEOLOCATE_TIMEOUT_MS = 30000;
const GEOLOCATION_REQUEST_OPTIONS = {
    enableHighAccuracy: true,
    timeout: GEOLOCATE_TIMEOUT_MS,
    maximumAge: 120000,
};
const GEOLOCATION_CACHED_FIRST_OPTIONS = {
    enableHighAccuracy: false,
    timeout: 1500,
    maximumAge: 1800000,
};
const GEOLOCATION_FALLBACK_OPTIONS = {
    enableHighAccuracy: false,
    timeout: 2000,
    maximumAge: 900000,
};
const GEOLOCATION_WATCH_OPTIONS = {
    enableHighAccuracy: false,
    // No explicit timeout here. On some mobile browsers watchPosition can
    // repeatedly timeout and generate noisy errors even when permission is fine.
    maximumAge: 120000,
};
const INFO_PANEL_STORAGE_KEY = 'troleum_info_panel_open';
const SORT_OPTION_STORAGE_KEY = 'troleum_sort_option';
const INFO_PANEL_MOBILE_BREAKPOINT = 900;
const SHOULD_LOG_TO_CONSOLE = window.location.hostname === '127.0.0.1';
let isInfoPanelOpen = true;
let isLocatingUser = false;
let locatingUserTimeoutId = null;

function localConsole(method, ...args) {
    if (!SHOULD_LOG_TO_CONSOLE) {
        return;
    }

    const logger = console?.[method];
    if (typeof logger === 'function') {
        logger(...args);
    }
}

const MAP_DARK_STYLE_FALLBACK = [
    { elementType: 'geometry', stylers: [{ color: '#1f2630' }] },
    { elementType: 'labels.text.fill', stylers: [{ color: '#b7c3d0' }] },
    { elementType: 'labels.text.stroke', stylers: [{ color: '#1f2630' }] },
    { featureType: 'administrative', elementType: 'geometry', stylers: [{ color: '#3d4a59' }] },
    { featureType: 'poi', elementType: 'geometry', stylers: [{ color: '#2a333f' }] },
    { featureType: 'road', elementType: 'geometry', stylers: [{ color: '#2f3a47' }] },
    { featureType: 'road.highway', elementType: 'geometry', stylers: [{ color: '#3b4b5c' }] },
    { featureType: 'transit', elementType: 'geometry', stylers: [{ color: '#2a313a' }] },
    { featureType: 'water', elementType: 'geometry', stylers: [{ color: '#0f2b45' }] },
];

function applyMapThemeFromSystem() {
    if (!map) {
        return;
    }

    if (GOOGLE_MAPS_MAP_ID) {
        return;
    }

    const prefersDark = window.matchMedia(MAP_THEME_MEDIA_QUERY).matches;

    // Explicit styles update reliably when the system theme changes at runtime.
    map.setOptions({ styles: prefersDark ? MAP_DARK_STYLE_FALLBACK : null });
}

function getLocationInputElement() {
    return locationInputElement || document.getElementById('location-input');
}

function setLocationInputValue(value) {
    const input = getLocationInputElement();
    if (!input) {
        return;
    }

    if ('value' in input) {
        input.value = value;
        return;
    }

    input.setAttribute('value', value);
}

function setLocationInputPlaceholder(value) {
    const input = getLocationInputElement();
    if (!input) {
        return;
    }

    if ('placeholder' in input) {
        input.placeholder = value;
    } else {
        input.setAttribute('placeholder', value);
    }
}

function setLocationInputDisabled(isDisabled) {
    const input = getLocationInputElement();
    if (!input) {
        return;
    }

    if ('disabled' in input) {
        input.disabled = isDisabled;
    } else if (isDisabled) {
        input.setAttribute('disabled', '');
    } else {
        input.removeAttribute('disabled');
    }
}

function setLocationInputOpacity(opacity) {
    const input = getLocationInputElement();
    if (!input) {
        return;
    }

    input.style.opacity = opacity;
}

function focusLocationInput() {
    const input = getLocationInputElement();
    if (!input || typeof input.focus !== 'function') {
        return;
    }

    input.focus();
}

function getMapCenterLiteral(targetMap = map) {
    const center = targetMap?.getCenter?.();
    if (!center) {
        return null;
    }

    const lat = typeof center.lat === 'function' ? center.lat() : center.lat;
    const lng = typeof center.lng === 'function' ? center.lng() : center.lng;
    if (!Number.isFinite(lat) || !Number.isFinite(lng)) {
        return null;
    }

    return { lat, lng };
}

function getCurrentMapViewState() {
    const center = getMapCenterLiteral();
    const zoom = map?.getZoom?.();

    return {
        center: center || { lat: 54.23782, lng: -4.555111 },
        zoom: Number.isFinite(zoom) ? zoom : 6,
    };
}

function buildMapOptions(viewState = null) {
    const mapOptions = {
        zoom: viewState?.zoom ?? 6,
        center: viewState?.center ?? { lat: 54.23782, lng: -4.555111 },
        mapTypeControl: true,
        streetViewControl: false,
        clickableIcons: false,
        //renderingType: RenderingType.VECTOR,
    };

    if (GOOGLE_MAPS_MAP_ID) {
        mapOptions.mapId = GOOGLE_MAPS_MAP_ID;

        const colorScheme = google.maps.ColorScheme;
        if (colorScheme?.DARK && colorScheme?.LIGHT) {
            mapOptions.colorScheme = window.matchMedia(MAP_THEME_MEDIA_QUERY).matches
                ? colorScheme.DARK
                : colorScheme.LIGHT;
        }
    }

    return mapOptions;
}

function createMapInstance(viewState = null) {
    map = new google.maps.Map(document.getElementById('map'), buildMapOptions(viewState));

    if (!GOOGLE_MAPS_MAP_ID) {
        applyMapThemeFromSystem();
    }
}

function applyAutocompleteRestriction(targetAutocomplete) {
    if (!targetAutocomplete) {
        return;
    }

    targetAutocomplete.includedRegionCodes = ['gb'];
    targetAutocomplete.locationRestriction = UK_AUTOCOMPLETE_BOUNDS;
}

function buildLocationMarkerOptions(position, title, zIndex, color, markerShape = 'pin') {
    return {
        position,
        map,
        title,
        zIndex,
        markerShape,
        pinOptions: {
            scale: 0.8,
            background: color,
            borderColor: '#ffffff',
            glyphColor: color,
        },
    };
}

function rebuildMapForThemeChange() {
    if (!map) {
        return;
    }

    if (!GOOGLE_MAPS_MAP_ID) {
        applyMapThemeFromSystem();
        return;
    }

    const viewState = getCurrentMapViewState();
    const previousSearchMarker = markersById.get('search-location');
    const previousSearchPosition = previousSearchMarker ? getMarkerPosition(previousSearchMarker) : null;
    const selectedMarkerId = selectedStationMarkerId;
    const routedStationId = activeRouteStationId;

    clearTimeout(debounceTimer);
    abortController?.abort();
    clearActiveRoutePolylines();

    markersById.forEach(marker => {
        removeMarker(marker);
    });
    infoWindowsById.forEach(infoWindow => {
        infoWindow.close();
    });

    markersById = new Map();
    infoWindowsById = new Map();

    createMapInstance(viewState);

    if (typeof requestStationsForCurrentView === 'function') {
        map.addListener('idle', requestStationsForCurrentView);
    }

    if (placesAutocomplete) {
        applyAutocompleteRestriction(placesAutocomplete);
    }

    if (previousSearchPosition) {
        const lat = typeof previousSearchPosition.lat === 'function' ? previousSearchPosition.lat() : previousSearchPosition.lat;
        const lng = typeof previousSearchPosition.lng === 'function' ? previousSearchPosition.lng() : previousSearchPosition.lng;

        if (Number.isFinite(lat) && Number.isFinite(lng)) {
            const rebuiltSearchMarker = createMapMarker(buildLocationMarkerOptions(
                { lat, lng },
                'Search Location',
                999999,
                MARKER_COLOR_DEFAULT,
                'circle',
            ));
            markersById.set('search-location', rebuiltSearchMarker);

            if (!searchLocationInfoWindow) {
                searchLocationInfoWindow = new google.maps.InfoWindow({
                    content: getSearchLocationInfoContent(),
                });
            }

            addMarkerClickListener(rebuiltSearchMarker, () => {
                openSearchLocationInfoWindow();
            });
        }
    }

    if (userLat != null && userLng != null) {
        const rebuiltUserMarker = createMapMarker(buildLocationMarkerOptions(
            { lat: userLat, lng: userLng },
            'Your Location',
            USER_MARKER_Z_INDEX,
            MARKER_COLOR_DEFAULT,
            'circle',
        ));
        markersById.set('user-location', rebuiltUserMarker);

        if (!userLocationInfoWindow) {
            userLocationInfoWindow = new google.maps.InfoWindow({
                content: getUserLocationInfoContent(),
            });
        }

        addMarkerClickListener(rebuiltUserMarker, () => {
            openUserLocationInfoWindow();
        });
    }

    if (isFollowingMyLocation) {
        setFollowMeMode();
    } else if (previousSearchPosition) {
        const lat = typeof previousSearchPosition.lat === 'function' ? previousSearchPosition.lat() : previousSearchPosition.lat;
        const lng = typeof previousSearchPosition.lng === 'function' ? previousSearchPosition.lng() : previousSearchPosition.lng;
        if (Number.isFinite(lat) && Number.isFinite(lng)) {
            setSearchLocationMode(lat, lng);
        }
    } else {
        updateFollowMeUI();
    }

    if (latestPins) {
        renderPins(latestPins);
        renderStationInfo(latestPins);
    }

    if (routedStationId) {
        showRouteForStation(routedStationId);
        return;
    }

    if (selectedMarkerId) {
        openStationInfoWindow(selectedMarkerId);
    }
}

function handleMapThemeChange() {
    if (GOOGLE_MAPS_MAP_ID) {
        rebuildMapForThemeChange();
        return;
    }

    applyMapThemeFromSystem();
}

function loadSortOptionPreference() {
    try {
        return localStorage.getItem(SORT_OPTION_STORAGE_KEY);
    } catch {
        return null;
    }
}

function persistSortOptionPreference(value) {
    try {
        localStorage.setItem(SORT_OPTION_STORAGE_KEY, value || 'distance');
    } catch {
        // Ignore storage failures (private mode, blocked storage, etc.)
    }
}

function startLocatingUser() {
    isLocatingUser = true;
    if (locatingUserTimeoutId) {
        clearTimeout(locatingUserTimeoutId);
    }

    locatingUserTimeoutId = setTimeout(() => {
        if (!isLocatingUser) {
            return;
        }

        localConsole('warn', 'Geolocation request timed out, location button re-enabled');

        if (navigator.permissions?.query) {
            navigator.permissions.query({ name: 'geolocation' }).then(result => {
                localConsole('warn', `[GEO] Permission state after timeout: ${result.state}`);
            }).catch(() => {
                // Ignore permissions API errors; not supported in some browsers.
            });
        }

        if (navigator.geolocation) {
            navigator.geolocation.getCurrentPosition((position) => {
                userLat = position.coords.latitude;
                userLng = position.coords.longitude;
                applyPendingFollowMeLocation(userLat, userLng);
                stopLocatingUser();
            }, (error) => {
                // Cached fallback can legitimately timeout quickly when no cached position exists.
                if (error?.code !== 3) {
                    logGeolocationError('fallback getCurrentPosition', error);
                }
                stopLocatingUser();
            }, GEOLOCATION_FALLBACK_OPTIONS);
            return;
        }

        stopLocatingUser();
    }, GEOLOCATE_TIMEOUT_MS);

}

function stopLocatingUser() {
    isLocatingUser = false;
    if (locatingUserTimeoutId) {
        clearTimeout(locatingUserTimeoutId);
        locatingUserTimeoutId = null;
    }
}

function logGeolocationError(context, error) {
    const code = error?.code;
    const message = error?.message || 'Unknown geolocation error';

    switch (code) {
    case 1:
        localConsole('warn', `[GEO] ${context}: permission denied (${message})`);
        break;
    case 2:
        localConsole('warn', `[GEO] ${context}: position unavailable (${message})`);
        break;
    case 3:
        localConsole('warn', `[GEO] ${context}: request timed out (${message})`);
        break;
    default:
        localConsole('warn', `[GEO] ${context}: ${message}`);
    }
}

function isMobileLayout() {
    return window.innerWidth <= INFO_PANEL_MOBILE_BREAKPOINT;
}

function loadInfoPanelState() {
    const storedValue = localStorage.getItem(INFO_PANEL_STORAGE_KEY);
    if (storedValue === 'open') {
        isInfoPanelOpen = true;
        return;
    }

    if (storedValue === 'closed') {
        isInfoPanelOpen = false;
        return;
    }

    isInfoPanelOpen = !isMobileLayout();
}

function persistInfoPanelState() {
    localStorage.setItem(INFO_PANEL_STORAGE_KEY, isInfoPanelOpen ? 'open' : 'closed');
}

function applyInfoPanelState() {
    const body = document.body;
    const menuToggle = document.getElementById('menu-toggle');

    if (!body || !menuToggle) {
        return;
    }

    const mobile = isMobileLayout();

    body.classList.toggle('mobile-layout', mobile);
    body.classList.toggle('desktop-layout', !mobile);
    body.classList.toggle('info-open', isInfoPanelOpen);
    body.classList.toggle('info-closed', !isInfoPanelOpen);
    menuToggle.classList.toggle('is-open', isInfoPanelOpen);

    menuToggle.setAttribute('aria-expanded', String(isInfoPanelOpen));
    menuToggle.setAttribute('aria-label', isInfoPanelOpen ? 'Hide station list panel' : 'Show station list panel');
}

function setInfoPanelOpen(isOpen) {
    isInfoPanelOpen = isOpen;
    persistInfoPanelState();
    applyInfoPanelState();
}

function toggleInfoPanel() {
    setInfoPanelOpen(!isInfoPanelOpen);
}

function closeInfoPanelForMapInteraction() {
    if (isMobileLayout() && isInfoPanelOpen) {
        setInfoPanelOpen(false);
    }
}

function initInfoPanelControls() {
    const menuToggle = document.getElementById('menu-toggle');
    const infoPanel = document.getElementById('info');

    if (!menuToggle || !infoPanel) {
        return;
    }

    menuToggle.addEventListener('click', event => {
        event.preventDefault();
        toggleInfoPanel();
    });

    window.addEventListener('resize', () => {
        applyInfoPanelState();
    });

    document.addEventListener('pointerdown', event => {
        if (!isMobileLayout() || !isInfoPanelOpen) {
            return;
        }

        const target = event.target;
        if (!(target instanceof Node)) {
            return;
        }

        if (!infoPanel.contains(target) && !menuToggle.contains(target)) {
            setInfoPanelOpen(false);
        }
    });

    document.addEventListener('keydown', event => {
        if (event.key === 'Escape' && isInfoPanelOpen) {
            setInfoPanelOpen(false);
        }
    });

    loadInfoPanelState();
    applyInfoPanelState();
}

function createMapMarkerContent(pinOptions = {}) {
    const PinElement = google.maps.marker?.PinElement || google.maps.PinElement;
    if (typeof PinElement !== 'function') {
        return null;
    }

    const pin = new PinElement(pinOptions);
    return pin || null;
}

function createCircleMarkerContent(color, size = 16) {
    const markerEl = document.createElement('div');
    markerEl.style.width = `${size}px`;
    markerEl.style.height = `${size}px`;
    markerEl.style.borderRadius = '50%';
    markerEl.style.backgroundColor = color;
    markerEl.style.border = '2px solid #ffffff';
    markerEl.style.boxSizing = 'border-box';
    markerEl.style.boxShadow = '0 0 0 1px rgba(0, 0, 0, 0.08)';
    return markerEl;
}

function createMapMarker({ map, position, title, zIndex, pinOptions, markerShape = 'pin' }) {
    const AdvancedMarkerElement = google.maps.marker?.AdvancedMarkerElement || google.maps.AdvancedMarkerElement;
    if (typeof AdvancedMarkerElement !== 'function') {
        throw new Error('AdvancedMarkerElement is unavailable; ensure the marker library is loaded.');
    }

    const markerContent = markerShape === 'circle'
        ? createCircleMarkerContent(pinOptions?.background || MARKER_COLOR_DEFAULT)
        : createMapMarkerContent(pinOptions);

    const marker = new AdvancedMarkerElement({
        map,
        position,
        title,
        zIndex,
        gmpClickable: true,
        content: markerContent,
    });

    marker.__isAdvancedMarker = true;
    marker.__defaultMap = map;
    marker.__markerShape = markerShape;
    return marker;
}

function setMarkerPosition(marker, position) {
    if (marker?.__isAdvancedMarker) {
        marker.position = position;
    }
}

function getMarkerPosition(marker) {
    if (marker?.__isAdvancedMarker) {
        return marker.position;
    }

    return null;
}

function setMarkerTitle(marker, title) {
    if (marker?.__isAdvancedMarker) {
        marker.title = title;
    }
}

function setMarkerZIndex(marker, zIndex) {
    if (marker?.__isAdvancedMarker) {
        marker.zIndex = zIndex;
    }
}

function setMarkerVisible(marker, isVisible) {
    if (marker?.__isAdvancedMarker) {
        marker.map = isVisible ? marker.__defaultMap : null;
    }
}

function setMarkerColor(marker, color) {
    if (!marker) {
        return;
    }

    if (marker.__isAdvancedMarker) {
        if (marker.__markerShape === 'circle') {
            marker.content = createCircleMarkerContent(color);
        } else {
            marker.content = createMapMarkerContent({
                scale: 1,
                background: color,
                borderColor: '#ffffff',
                glyphColor: color,
            });
        }
    }
}

function removeMarker(marker) {
    if (marker?.__isAdvancedMarker) {
        marker.map = null;
    }
}

function addMarkerClickListener(marker, handler) {
    if (marker?.__isAdvancedMarker && typeof marker.addEventListener === 'function') {
        marker.addEventListener('gmp-click', handler);
        return;
    }

    marker?.addListener?.('click', handler);
}

function updateFollowMeUI() {
    setLocationInputDisabled(false);
    setLocationInputOpacity('1');
}

function populateFollowMeLocationInput(lat, lng) {
    const geocoder = new google.maps.Geocoder();

    geocoder.geocode({ location: { lat, lng } }, (results, status) => {
        if (isFollowingMyLocation && status === 'OK' && results[0]) {
            userEstimatedAddress = results[0].formatted_address;
            setLocationInputValue(userEstimatedAddress);
        } else if (status !== 'OK') {
            localConsole('warn', 'Reverse geocoding failed:', status);
        }

        setLocationInputPlaceholder('Enter a location');
    });
}

function getUserLocationInfoContent() {
    const safeAddress = escapeHtml(userEstimatedAddress || 'Address unavailable');
    return `
        <div class="info-window">
            <h3>My location</h3>
            <p class="address">📍 ${safeAddress}</p>
        </div>
    `;
}

function openUserLocationInfoWindow() {
    const userMarker = markersById.get('user-location');
    if (!userMarker || !userLocationInfoWindow) {
        return;
    }

    userLocationInfoWindow.setContent(getUserLocationInfoContent());
    userLocationInfoWindow.open({ map, anchor: userMarker });
}

function populateSearchLocationAddress(lat, lng) {
    const geocoder = new google.maps.Geocoder();
    geocoder.geocode({ location: { lat, lng } }, (results, status) => {
        if (status === 'OK' && results[0]) {
            searchSetAddress = results[0].formatted_address;
        } else if (status !== 'OK') {
            localConsole('warn', 'Search location reverse geocoding failed:', status);
        }
    });
}

function getSearchLocationInfoContent() {
    const safeAddress = escapeHtml(searchSetAddress || 'Address unavailable');
    return `
        <div class="info-window">
            <h3>Set location</h3>
            <p class="address">📍 ${safeAddress}</p>
        </div>
    `;
}

function openSearchLocationInfoWindow() {
    const searchMarker = markersById.get('search-location');
    if (!searchMarker || !searchLocationInfoWindow) {
        return;
    }

    searchLocationInfoWindow.setContent(getSearchLocationInfoContent());
    searchLocationInfoWindow.open({ map, anchor: searchMarker });
}

function applyPendingFollowMeLocation(lat, lng) {
    if (!pendingFollowMeLocationRequest || !isFollowingMyLocation) {
        return;
    }

    pendingFollowMeLocationRequest = false;
    map.setCenter({ lat, lng });
    map.setZoom(13);
    populateFollowMeLocationInput(lat, lng);
}

function setFollowMeMode() {
    isFollowingMyLocation = true;
    setLocationInputValue('');
    setLocationInputPlaceholder('Enter a location');

    markersById.forEach((marker, id) => {
        if (id === 'search-location') {
            setMarkerVisible(marker, false);
        } else if (id === 'user-location') {
            setMarkerVisible(marker, true);
            setMarkerColor(marker, MARKER_COLOR_DEFAULT);
            setMarkerZIndex(marker, USER_MARKER_Z_INDEX);
        }
    });

    updateFollowMeUI();
}

function setSearchLocationMode(lat, lng) {
    isFollowingMyLocation = false;

    markersById.forEach((marker, id) => {
        if (id === 'user-location') {
            setMarkerVisible(marker, false);
        } else if (id === 'search-location') {
            setMarkerVisible(marker, true);
            setMarkerColor(marker, MARKER_COLOR_DEFAULT);
            setMarkerZIndex(marker, USER_MARKER_Z_INDEX);
        }
    });

    updateFollowMeUI();
}

function getStationRequestOrigin() {
    if (isFollowingMyLocation) {
        if (userLat != null && userLng != null) {
            return { lat: userLat, lng: userLng };
        }
        return null;
    }

    const searchMarker = markersById.get('search-location');
    const searchPosition = searchMarker ? getMarkerPosition(searchMarker) : null;
    if (searchPosition) {
        const lat = typeof searchPosition.lat === 'function' ? searchPosition.lat() : searchPosition.lat;
        const lng = typeof searchPosition.lng === 'function' ? searchPosition.lng() : searchPosition.lng;
        if (Number.isFinite(lat) && Number.isFinite(lng)) {
            return { lat, lng };
        }
    }

    if (userLat != null && userLng != null) {
        return { lat: userLat, lng: userLng };
    }

    return null;
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
            localConsole('error', 'Failed to preload fuel types:', err);
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

function buildMarkerColorByPinId(pinList, selectedFuelType) {
    const colorByPinId = new Map();

    if (!selectedFuelType) {
        pinList.forEach(pin => {
            colorByPinId.set(String(pin.id), MARKER_COLOR_DEFAULT);
        });
        return colorByPinId;
    }

    const pricedPins = pinList.map(pin => ({
        id: String(pin.id),
        price: getFuelPriceForType(pin, selectedFuelType),
    })).filter(entry => entry.price != null);

    if (pricedPins.length === 0) {
        pinList.forEach(pin => {
            colorByPinId.set(String(pin.id), MARKER_COLOR_EXPENSIVE);
        });
        return colorByPinId;
    }

    const sortedPrices = pricedPins.map(entry => entry.price).sort((a, b) => a - b);
    const lowThreshold = sortedPrices[Math.floor((sortedPrices.length - 1) / 3)];
    const highThreshold = sortedPrices[Math.floor(((sortedPrices.length - 1) * 2) / 3)];

    pricedPins.forEach(entry => {
        if (entry.price <= lowThreshold) {
            colorByPinId.set(entry.id, MARKER_COLOR_CHEAPEST);
            return;
        }
        if (entry.price <= highThreshold) {
            colorByPinId.set(entry.id, MARKER_COLOR_MEDIUM);
            return;
        }
        colorByPinId.set(entry.id, MARKER_COLOR_EXPENSIVE);
    });

    pinList.forEach(pin => {
        const id = String(pin.id);
        if (!colorByPinId.has(id)) {
            colorByPinId.set(id, MARKER_COLOR_EXPENSIVE);
        }
    });

    return colorByPinId;
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

function getDistanceHtmlForPin(pin, isDistanceSelected) {
    const routeDistance = routeDistanceMilesByStationId.get(String(pin?.id));
    if (routeDistance != null) {
        return getDistanceHtml(routeDistance, isDistanceSelected).replace('Distance:', 'Route distance:');
    }

    return getDistanceHtml(pin?.distance, isDistanceSelected);
}

function formatMilesFromMeters(meters) {
    if (!Number.isFinite(meters)) {
        return null;
    }

    return meters * 0.000621371;
}

function clearActiveRoutePolylines() {
    activeRoutePolylines.forEach(polyline => {
        polyline.setMap(null);
    });
    activeRoutePolylines = [];
}

function extractRouteDistanceMiles(route) {
    const legs = Array.isArray(route?.legs) ? route.legs : [];
    const totalMeters = legs.reduce((sum, leg) => {
        const meters = Number(leg?.distanceMeters);
        return Number.isFinite(meters) ? sum + meters : sum;
    }, 0);

    if (totalMeters > 0) {
        return formatMilesFromMeters(totalMeters);
    }

    return null;
}

function getCurrentRouteOrigin() {
    if (isFollowingMyLocation && userLat != null && userLng != null) {
        return { lat: userLat, lng: userLng };
    }

    const searchMarker = markersById.get('search-location');
    if (searchMarker && !isFollowingMyLocation) {
        const position = getMarkerPosition(searchMarker);
        if (position) {
            const lat = typeof position.lat === 'function' ? position.lat() : position.lat;
            const lng = typeof position.lng === 'function' ? position.lng() : position.lng;
            if (Number.isFinite(lat) && Number.isFinite(lng)) {
                return { lat, lng };
            }
        }
    }

    if (userLat != null && userLng != null) {
        return { lat: userLat, lng: userLng };
    }

    return null;
}

function getPinById(markerId) {
    const pinList = Array.isArray(latestPins?.data) ? latestPins.data : [];
    return pinList.find(pin => String(pin.id) === String(markerId)) || null;
}

async function initDirections() {
    const { Route } = await google.maps.importLibrary('routes');
    routesApiRouteClass = Route;
}

async function requestRoute(origin, destination) {
    if (!routesApiRouteClass) {
        throw new Error('Routes API is not initialized yet');
    }

    const request = {
        origin,
        destination,
        travelMode: 'DRIVING',
        fields: ['legs', 'path', 'viewport'],
    };

    const result = await routesApiRouteClass.computeRoutes(request);
    if (!result?.routes || result.routes.length === 0) {
        throw new Error('No routes found');
    }

    return result.routes[0];
}

async function showRouteForStation(markerId) {
    if (!routesApiRouteClass) {
        localConsole('warn', 'Routes API is not initialized yet');
        return;
    }

    infoWindowsById.forEach(iw => iw.close());
    updateSelectedLocationListItem(null);

    const pin = getPinById(markerId);
    if (!pin || pin.lat == null || pin.lng == null) {
        return;
    }

    const origin = getCurrentRouteOrigin();
    if (!origin) {
        alert('Location not available yet. Please allow location access or search for a location first.');
        return;
    }

    try {
        const route = await requestRoute(origin, { lat: pin.lat, lng: pin.lng });
        const routeMiles = extractRouteDistanceMiles(route);
        activeRouteStationId = String(pin.id);

        clearActiveRoutePolylines();
        activeRoutePolylines = route.createPolylines({
            strokeColor: '#1A73E8',
            strokeOpacity: 0.9,
            strokeWeight: 6,
        });
        activeRoutePolylines.forEach(polyline => {
            polyline.setMap(map);
        });

        if (route.viewport) {
            map.fitBounds(route.viewport, 50);
        }

        if (routeMiles != null) {
            routeDistanceMilesByStationId.set(String(pin.id), routeMiles);
        }

        if (latestPins) {
            renderPins(latestPins);
            renderStationInfo(latestPins);
        }
    } catch (err) {
        activeRouteStationId = null;
        localConsole('error', err);
        alert('Could not calculate a driving route for this station.');
    }
}

function updateSortOptionsFromPins(pinList) {
    const sortSelect = document.getElementById('sort-options');
    const currentValue = sortSelect.value || 'distance';
    const storedValue = loadSortOptionPreference();
    const previousValue = currentValue !== 'distance' ? currentValue : (storedValue || currentValue);

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
    persistSortOptionPreference(sortSelect.value);
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
    const addressText = pin?.address || 'No address available';
    if (pin.lat == null || pin.lng == null) {
        return escapeHtml(addressText);
    }

    const mapsUrl = getMapsUrl(pin.lat, pin.lng, pin?.name || addressText);
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
    map.panTo(getMarkerPosition(marker));
    infoWindow.open({ map, anchor: marker });
}

function initMap() {
    createMapInstance();

    const themeMediaQuery = window.matchMedia(MAP_THEME_MEDIA_QUERY);
    if (typeof themeMediaQuery.addEventListener === 'function') {
        themeMediaQuery.addEventListener('change', handleMapThemeChange);
    } else if (typeof themeMediaQuery.addListener === 'function') {
        themeMediaQuery.addListener(handleMapThemeChange);
    }

    initDirections().catch(err => {
        localConsole('error', 'Failed to initialize Routes API:', err);
    });

    // Constantly update a blue dot on the map showing the user's current location, and center the map on it when the page loads
    if (navigator.geolocation) {
        navigator.geolocation.watchPosition((position) => {
            const lat = position.coords.latitude;
            const lon = position.coords.longitude;

            // Cache latest coordinates for station API requests.
            userLat = lat;
            userLng = lon;
            stopLocatingUser();

            // Create or update a marker for the user's location
            if (!markersById.has('user-location')) {
                const userMarker = createMapMarker(buildLocationMarkerOptions(
                    { lat, lng: lon },
                    'Your Location',
                    USER_MARKER_Z_INDEX,
                    MARKER_COLOR_DEFAULT,
                    'circle',
                ));
                markersById.set('user-location', userMarker);

                if (!userLocationInfoWindow) {
                    userLocationInfoWindow = new google.maps.InfoWindow({
                        content: getUserLocationInfoContent(),
                    });
                }

                addMarkerClickListener(userMarker, () => {
                    openUserLocationInfoWindow();
                });
            } else {
                const userMarker = markersById.get('user-location');
                setMarkerPosition(userMarker, { lat, lng: lon });
                setMarkerZIndex(userMarker, USER_MARKER_Z_INDEX);
                setMarkerColor(userMarker, MARKER_COLOR_DEFAULT);
                if (isFollowingMyLocation) {
                    setMarkerVisible(userMarker, true);
                }
            }

            applyPendingFollowMeLocation(lat, lon);
        }, (error) => {
            // Ignore watch timeout noise; user-triggered getCurrentPosition handles first-fix UX.
            if (error?.code !== 3) {
                logGeolocationError('watchPosition', error);
            }
        }, GEOLOCATION_WATCH_OPTIONS);
    }

    // Fire-and-forget preload: starts immediately but never blocks map/markers rendering.
    preloadFuelTypes().then(() => {
        if (latestPins) {
            renderStationInfo(latestPins);
        }
    });

    // Create bounds to fit all markers
    //const bounds = new google.maps.LatLngBounds();

    requestStationsForCurrentView = function requestStationsForCurrentViewImpl() {
        clearTimeout(debounceTimer);

        const url = new URL('/api/stations', window.location.origin);
        const requestOrigin = getStationRequestOrigin();
        if (requestOrigin) {
            url.searchParams.set('lat', requestOrigin.lat);
            url.searchParams.set('lng', requestOrigin.lng);
        }

        const selectedFuelType = getSelectedFuelSortValue();
        if (selectedFuelType) {
            url.searchParams.set('fuel_type', selectedFuelType);
        }

        const bounds = map.getBounds();
        if (!bounds) {
            return;
        }
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
                localConsole('error', err);
            }
        }, 200);
    };

    map.addListener('idle', requestStationsForCurrentView);

    const legacyInput = document.getElementById('location-input');
    const sortSelect = document.getElementById('sort-options');

    sortSelect.addEventListener('change', () => {
        persistSortOptionPreference(sortSelect.value || 'distance');

        // Re-render immediately from cached pins so marker categories update even
        // if the network request is slow or aborted.
        if (latestPins) {
            renderPins(latestPins);
            renderStationInfo(latestPins);
        }

        requestStationsForCurrentView();
    });

    const placeAutocomplete = new google.maps.places.PlaceAutocompleteElement();
    placeAutocomplete.id = 'location-input';
    placeAutocomplete.setAttribute('placeholder', 'Enter a location');
    placeAutocomplete.setAttribute('aria-label', 'Enter a location');
    applyAutocompleteRestriction(placeAutocomplete);

    if (legacyInput?.parentNode) {
        legacyInput.parentNode.replaceChild(placeAutocomplete, legacyInput);
    }

    placesAutocomplete = placeAutocomplete;
    locationInputElement = placeAutocomplete;

    placeAutocomplete.addEventListener('gmp-select', async event => {
        const placePrediction = event.placePrediction || event.detail?.placePrediction;
        if (!placePrediction?.toPlace) {
            alert('No details available for this location');
            return;
        }

        const place = placePrediction.toPlace();
        await place.fetchFields({ fields: ['formattedAddress', 'displayName', 'location'] });

        const location = place.location;
        if (!location) {
            alert('No details available for this location');
            return;
        }

        const lat = typeof location.lat === 'function' ? location.lat() : location.lat;
        const lng = typeof location.lng === 'function' ? location.lng() : location.lng;
        if (!Number.isFinite(lat) || !Number.isFinite(lng)) {
            alert('No details available for this location');
            return;
        }

        searchSetAddress = place.formattedAddress || place.displayName || null;
        if (!searchSetAddress) {
            populateSearchLocationAddress(lat, lng);
        }

        if (!markersById.has('search-location')) {
            const searchMarker = createMapMarker(buildLocationMarkerOptions(
                { lat, lng },
                'Search Location',
                USER_MARKER_Z_INDEX,
                MARKER_COLOR_CHEAPEST,
                'circle',
            ));
            markersById.set('search-location', searchMarker);

            if (!searchLocationInfoWindow) {
                searchLocationInfoWindow = new google.maps.InfoWindow({
                    content: getSearchLocationInfoContent(),
                });
            }

            addMarkerClickListener(searchMarker, () => {
                openSearchLocationInfoWindow();
            });
        } else {
            const searchMarker = markersById.get('search-location');
            searchMarker.__markerShape = 'circle';
            setMarkerColor(searchMarker, MARKER_COLOR_CHEAPEST);
            setMarkerPosition(searchMarker, { lat, lng });
            setMarkerVisible(searchMarker, true);
        }

        setSearchLocationMode(lat, lng);
        requestStationsForCurrentView?.();

        // Center map on the selected marker itself.
        map.setCenter({ lat, lng });
        map.setZoom(13);
    });

    centerMapOnUserLocation();
}

function renderPins(pins) {
    const pinList = Array.isArray(pins?.data) ? pins.data : [];
    localConsole('warn', `Fetched ${pinList.length} pins from the server`);
    updateSortOptionsFromPins(pinList);

    const selectedFuelType = getSelectedFuelSortValue();
    const isDistanceSelected = selectedFuelType == null;
    const markerColorByPinId = buildMarkerColorByPinId(pinList, selectedFuelType);

    const nextIds = new Set(pinList.map(pin => String(pin.id)));

    // Remove markers that are no longer in the latest results.
    markersById.forEach((marker, id) => {
        if (id === 'user-location') return; // never remove the user location marker
        if (id === 'search-location') return; // never remove the search location marker
        if (!nextIds.has(id)) {
            removeMarker(marker);
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
        const stationName = pin?.name || 'Unnamed Station';
        const safeStationName = escapeHtml(stationName);
        const phoneHtml = pin?.phone && pin?.phone_uri
            ? `<a href="${escapeHtml(pin.phone_uri)}">${escapeHtml(pin.phone)}</a>`
            : (pin?.phone ? escapeHtml(pin.phone) : 'No phone available');
        if (id === 'user-location') return; // never overwrite the user location marker
        const infoContent = `
            <div class="info-window">
                <h3>${safeStationName}</h3><br />
                <p class="distance">${getDistanceHtmlForPin(pin, isDistanceSelected)}</p><br />
                <div class="prices-header">⛽ Prices:</div>
                <table class="prices"><thead><tr><th>Fuel type</th><th>Price</th></tr></thead><tbody>${sortedPrices.length > 0 ? sortedPrices.map(p => `<tr><td>${getFuelTypeLabelHtml(p.fuel_type, selectedFuelType)}</td><td>${getFuelPriceHtml(p.fuel_type, p.price, selectedFuelType)}</td></tr>`).join('') : '<tr><td colspan="2">No price data available</td></tr>'}</tbody></table><br />
                <p class="address">📍 Address:<br />${buildAddressLinkHtml(pin)}</p><br />
                <p class="phone">📞 Telephone:<br />${phoneHtml}</p><br />
                <p><a href="#" class="info-window-route-link" data-route-id="${escapeHtml(String(pin.id))}">Show driving route on map</a></p>
            </div>
        `;

        if (markersById.has(id)) {
            // Keep existing marker and update mutable fields.
            const existingMarker = markersById.get(id);
            setMarkerPosition(existingMarker, { lat: pin.lat, lng: pin.lng });
            setMarkerTitle(existingMarker, stationName);
            setMarkerColor(existingMarker, markerColorByPinId.get(id) || MARKER_COLOR_DEFAULT);

            const existingInfoWindow = infoWindowsById.get(id);
            if (existingInfoWindow) {
                existingInfoWindow.setContent(infoContent);
            }

            bounds.extend(getMarkerPosition(existingMarker));
            return;
        }

        const marker = createMapMarker({
            position: { lat: pin.lat, lng: pin.lng },
            map,
            title: stationName,
            markerShape: 'pin',
            pinOptions: {
                scale: 1,
                background: markerColorByPinId.get(id) || MARKER_COLOR_DEFAULT,
                borderColor: '#ffffff',
                glyphColor: markerColorByPinId.get(id) || MARKER_COLOR_DEFAULT,
            },
        });

        const infoWindow = new google.maps.InfoWindow({
            content: infoContent
        });

        infoWindow.addListener('closeclick', function() {
            updateSelectedLocationListItem(null);
        });

        addMarkerClickListener(marker, function() {
            openStationInfoWindow(id);
        });

        bounds.extend(getMarkerPosition(marker));

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
        infoDiv.innerHTML = '<div class="location-list-item">No stations found in this area.</div>';
        return;
    }

    const stationInfoHtml = sortedPinList.map(pin => {
        const sortedPrices = getSortedPrices(pin.prices);
        const safeStationName = escapeHtml(pin?.name || 'Unnamed Station');

        return `
        <div class="location-list-item" data-marker-id="${escapeHtml(String(pin.id))}">
            <h3>${safeStationName}</h3>
            <p class="distance">${getDistanceHtmlForPin(pin, isDistanceSelected)}</p>
            <table class="prices"><thead><tr><th>Fuel type</th><th>Price</th></tr></thead><tbody>${sortedPrices.length > 0 ? sortedPrices.map(p => `<tr><td class="price-label">${getFuelTypeLabelHtml(p.fuel_type, selectedFuelType)}</td><td class="price-value">${getFuelPriceHtml(p.fuel_type, p.price, selectedFuelType)}</td></tr>`).join('') : '<tr><td colspan="2">No price data available</td></tr>'}</tbody></table>
            <p><a href="#" class="show-route-link" data-route-id="${escapeHtml(String(pin.id))}">Show driving route on map</a></p>
        </div>
    `;
    }).join('<hr>');

    infoDiv.innerHTML = stationInfoHtml;

    infoDiv.querySelectorAll('.location-list-item').forEach(item => {
        item.addEventListener('click', () => {
            openStationInfoWindow(item.dataset.markerId);
            closeInfoPanelForMapInteraction();
        });
    });

    infoDiv.querySelectorAll('.show-route-link').forEach(link => {
        link.addEventListener('click', event => {
            event.preventDefault();
            event.stopPropagation();
            closeInfoPanelForMapInteraction();
            showRouteForStation(link.dataset.routeId);
        });
    });

    updateSelectedLocationListItem(selectedStationMarkerId);
}

// Function to get the user's current location and center the map on it
function centerMapOnUserLocation() {
    if (isLocatingUser) {
        return;
    }

    setFollowMeMode();
    pendingFollowMeLocationRequest = true;

    if (navigator.geolocation) {
        startLocatingUser();
        localConsole('warn', 'Attempting to get user location via Geolocation API...');
        setLocationInputPlaceholder('Searching for your location, please wait');

        if (userLat !== null && userLng !== null) {
            applyPendingFollowMeLocation(userLat, userLng);
            stopLocatingUser();
            return;
        }

        // 1) Quick cached attempt first, then 2) longer live fix attempt.
        navigator.geolocation.getCurrentPosition((position) => {
            userLat = position.coords.latitude;
            userLng = position.coords.longitude;
            applyPendingFollowMeLocation(userLat, userLng);
            stopLocatingUser();
        }, () => {
            navigator.geolocation.getCurrentPosition((position) => {
                userLat = position.coords.latitude;
                userLng = position.coords.longitude;
                applyPendingFollowMeLocation(userLat, userLng);
                stopLocatingUser();
            }, (error) => {
                logGeolocationError('getCurrentPosition', error);
                stopLocatingUser();
            }, GEOLOCATION_REQUEST_OPTIONS);
        }, GEOLOCATION_CACHED_FIRST_OPTIONS);
    } else {
        stopLocatingUser();
        localConsole('warn', 'Geolocation is not supported by this browser, cannot center map on user location');
        setLocationInputPlaceholder('Enter a location');
    }
}

document.addEventListener('DOMContentLoaded', function() {
    document.addEventListener('click', event => {
        const routeLink = event.target?.closest?.('.info-window-route-link');
        if (!routeLink) {
            return;
        }

        event.preventDefault();
        event.stopPropagation();

        const routeId = routeLink.getAttribute('data-route-id');
        if (routeId) {
            showRouteForStation(routeId);
        }
    });

    updateFollowMeUI();
    initInfoPanelControls();
});

window.showRouteForStation = showRouteForStation;
window.toggleInfoPanel = toggleInfoPanel;
