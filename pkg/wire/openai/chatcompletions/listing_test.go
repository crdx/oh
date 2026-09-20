package chatcompletions_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"crdx.org/io/internal/req"
)

func TestModelsLeavesUnlistedCapabilitiesUnknown(t *testing.T) {
	var path string
	var authorisation string
	var accepted string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		path = request.URL.Path
		authorisation = request.Header.Get("Authorization")
		accepted = request.Header.Get("Accept")
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, `{"data":[{"id":"first"},{"id":""},{"id":"second"}]}`)
	}))
	t.Cleanup(server.Close)

	client := newClient(t, server.URL+"/v1/chat/completions")
	observer := &countingObserver{}
	client.ObserveHTTP(observer)

	models, err := client.Models(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if path != "/v1/models" {
		t.Errorf("got listing path %q", path)
	}
	if authorisation != "Bearer secret" || accepted != "application/json" {
		t.Errorf("got authorisation %q and accept %q", authorisation, accepted)
	}
	if observer.requests != 1 {
		t.Errorf("observed %d listing requests", observer.requests)
	}
	if len(models) != 2 || models[0].ID != "first" || models[1].ID != "second" {
		t.Fatalf("got models %+v", models)
	}
	if models[0].EffortLevels != nil || models[1].EffortLevels != nil {
		t.Errorf("the listing claimed effort levels it did not report: %+v", models)
	}
}

func TestModelsReturnsNothingWhenTheConversationURLCannotNameAListing(t *testing.T) {
	client := newClient(t, "http://127.0.0.1:1/not-chat-completions")

	models, err := client.Models(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if models != nil {
		t.Errorf("got models %+v", models)
	}
}

func TestModelsReportsARefusedListing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, `{"error":{"message":"listing refused"}}`, http.StatusForbidden)
	}))
	t.Cleanup(server.Close)

	client := newClient(t, server.URL+"/v1/chat/completions")
	_, err := client.Models(t.Context())

	refused, ok := errors.AsType[*req.StatusError](err)
	if !ok || refused.Status != http.StatusForbidden {
		t.Fatalf("got error %v", err)
	}
}
