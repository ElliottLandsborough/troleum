package main

import "testing"

func TestRetryQueueAddGetHasRequests(t *testing.T) {
	rq := &RetryQueue{requests: make([]RetryRequest, 0)}
	if rq.HasRequests() {
		t.Fatal("expected empty queue")
	}

	rq.AddRequest(1, true)
	rq.AddRequest(1, true)
	rq.AddRequest(2, false)

	if len(rq.requests) != 2 {
		t.Fatalf("expected duplicate request to be ignored, got %d requests", len(rq.requests))
	}
	if !rq.HasRequests() {
		t.Fatal("expected queue to have requests")
	}

	first, ok := rq.GetNextRequest()
	if !ok || first.PageNum != 1 || !first.IsStations {
		t.Fatalf("unexpected first request: %#v, ok=%v", first, ok)
	}
	second, ok := rq.GetNextRequest()
	if !ok || second.PageNum != 2 || second.IsStations {
		t.Fatalf("unexpected second request: %#v, ok=%v", second, ok)
	}
	if _, ok := rq.GetNextRequest(); ok {
		t.Fatal("expected queue to be empty after draining")
	}
}

func TestGetRequestType(t *testing.T) {
	if got := getRequestType(true); got != "STATIONS" {
		t.Fatalf("expected STATIONS, got %q", got)
	}
	if got := getRequestType(false); got != "PRICES" {
		t.Fatalf("expected PRICES, got %q", got)
	}
}
