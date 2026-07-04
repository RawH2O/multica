package notificationpush

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestBarkDeviceKeysFromEnv(t *testing.T) {
	t.Setenv("MULTICA_BARK_DEVICE_KEY", "one")
	t.Setenv("MULTICA_BARK_DEVICE_KEYS", " two, ,three ")

	got := barkDeviceKeysFromEnv()
	want := []string{"two", "three"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("barkDeviceKeysFromEnv() = %#v, want %#v", got, want)
	}
}

func TestPostBark(t *testing.T) {
	var got barkPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/push" {
			t.Fatalf("path = %q, want /push", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("content-type = %q, want application/json", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := postBark(context.Background(), srv.Client(), srv.URL, "device-key", "Issue updated", "New comment"); err != nil {
		t.Fatalf("postBark() error = %v", err)
	}

	want := barkPayload{
		DeviceKey: "device-key",
		Title:     "Issue updated",
		Body:      "New comment",
		Group:     "Multica",
	}
	if got != want {
		t.Fatalf("payload = %#v, want %#v", got, want)
	}
}

func TestPostBarkStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad key", http.StatusUnauthorized)
	}))
	defer srv.Close()

	if err := postBark(context.Background(), srv.Client(), srv.URL, "bad", "Title", ""); err == nil {
		t.Fatal("postBark() error = nil, want status error")
	}
}
