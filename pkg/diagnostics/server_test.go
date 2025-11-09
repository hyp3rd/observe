package diagnostics_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyp3rd/observe/pkg/config"
	"github.com/hyp3rd/observe/pkg/diagnostics"
)

const statusEndpoint = "/observe/status"

type stubSnapshotProvider struct {
	snapshot diagnostics.Snapshot
}

func (s stubSnapshotProvider) Snapshot() diagnostics.Snapshot {
	return s.snapshot
}

func TestHandleStatusReturnsSnapshot(t *testing.T) {
	t.Parallel()

	//nolint:revive
	provider := stubSnapshotProvider{
		snapshot: diagnostics.Snapshot{
			ServiceName: "test",
			TraceExporter: diagnostics.ExporterStatus{
				Protocol:      "grpc",
				Endpoint:      "collector:4317",
				LastError:     "boom",
				LastErrorTime: time.Date(2024, 12, 5, 12, 0, 0, 0, time.UTC),
			},
		},
	}
	server := diagnostics.NewServer(
		config.DiagnosticsConfig{
			Enabled:  true,
			HTTPAddr: "127.0.0.1:0",
		},
		provider,
	)

	req := httptest.NewRequest(http.MethodGet, statusEndpoint, nil)
	rr := httptest.NewRecorder()

	server.HandleStatus(rr, req)

	res := rr.Result()

	defer func() {
		err := res.Body.Close()
		if err != nil {
			t.Fatalf("close response body: %v", err)
		}
	}()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: got %d", res.StatusCode)
	}

	var snapshot diagnostics.Snapshot

	err := json.NewDecoder(res.Body).Decode(&snapshot)
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if snapshot.TraceExporter.Endpoint != "collector:4317" {
		t.Fatalf("expected endpoint collector:4317, got %s", snapshot.TraceExporter.Endpoint)
	}

	if snapshot.TraceExporter.LastError != "boom" {
		t.Fatalf("expected last error boom, got %s", snapshot.TraceExporter.LastError)
	}
}

func TestHandleStatusAuth(t *testing.T) {
	t.Parallel()

	server := diagnostics.NewServer(
		config.DiagnosticsConfig{
			AuthToken: "secret",
		},
		stubSnapshotProvider{},
	)

	req := httptest.NewRequest(http.MethodGet, statusEndpoint, nil)
	rr := httptest.NewRecorder()

	server.HandleStatus(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when missing auth, got %d", rr.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, statusEndpoint, bytes.NewBuffer(nil))
	req2.Header.Set("Authorization", "Bearer secret")

	rr2 := httptest.NewRecorder()
	server.HandleStatus(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("expected 200 with auth, got %d", rr2.Code)
	}
}
