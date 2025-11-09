// Package diagnostics provides a diagnostics server for exposing runtime status.
package diagnostics

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hyp3rd/ewrap"

	"github.com/hyp3rd/observe/internal/constants"
	"github.com/hyp3rd/observe/pkg/config"
)

// Snapshot captures the current runtime configuration for diagnostics endpoints.
type Snapshot struct {
	ServiceName       string          `json:"service_name"`
	ServiceVersion    string          `json:"service_version"`
	Environment       string          `json:"environment"`
	SamplingMode      string          `json:"sampling_mode"`
	ExporterEndpoint  string          `json:"exporter_endpoint"`
	StartTime         time.Time       `json:"start_time"`
	LastReloadTime    time.Time       `json:"last_reload_time"`
	Instrumentation   map[string]bool `json:"instrumentation"`
	ConfigReloadCount int64           `json:"config_reload_count"`
	TraceQueueLimit   int64           `json:"trace_queue_limit"`
	TraceDroppedSpans int64           `json:"trace_dropped_spans"`
	TraceExporter     ExporterStatus  `json:"trace_exporter"`
	Timestamp         time.Time       `json:"timestamp"`
}

// ExporterStatus describes exporter health for diagnostics.
type ExporterStatus struct {
	Protocol      string    `json:"protocol"`
	Endpoint      string    `json:"endpoint"`
	LastError     string    `json:"last_error"`
	LastErrorTime time.Time `json:"last_error_time"`
}

// SnapshotProvider supplies diagnostic snapshots.
type SnapshotProvider interface {
	Snapshot() Snapshot
}

// Server exposes runtime status over HTTP for operational diagnostics.
type Server struct {
	cfg      config.DiagnosticsConfig
	provider SnapshotProvider

	server *http.Server
	mu     sync.Mutex
	start  sync.Once
	stop   sync.Once
}

// NewServer constructs a diagnostics server.
func NewServer(cfg config.DiagnosticsConfig, provider SnapshotProvider) *Server {
	return &Server{
		cfg:      cfg,
		provider: provider,
	}
}

// Start begins serving the diagnostics endpoint until the supplied context is canceled or Shutdown is called.
func (s *Server) Start(ctx context.Context) error {
	if s.cfg.HTTPAddr == "" {
		return ewrap.New("diagnostics http_addr is required")
	}

	var startErr error

	s.start.Do(func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/observe/status", s.HandleStatus)

		s.server = &http.Server{
			Addr:              s.cfg.HTTPAddr,
			Handler:           mux,
			ReadHeaderTimeout: constants.DefaultTimeout,
		}

		lc := net.ListenConfig{}

		ln, err := lc.Listen(ctx, "tcp", s.cfg.HTTPAddr)
		if err != nil {
			startErr = ewrap.Wrap(err, "listen diagnostics")

			return
		}

		go func() {
			<-ctx.Done()

			shutdownCtx, cancel := context.WithTimeout(ctx, constants.DefaultShutdownTimeout)
			defer cancel()

			err = s.Shutdown(shutdownCtx)
			if err != nil {
				//nolint:errcheck // best-effort logging via stderr
				_ = ewrap.Wrap(err, "shutdown diagnostics server")
			}
		}()

		go func() {
			err := s.server.Serve(ln)
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				//nolint:errcheck // best-effort logging via stderr
				_ = ewrap.Wrap(err, "diagnostics server stopped")
			}
		}()
	})

	return startErr
}

// Shutdown stops the diagnostics server gracefully.
func (s *Server) Shutdown(ctx context.Context) error {
	var shutdownErr error

	s.stop.Do(func() {
		s.mu.Lock()
		defer s.mu.Unlock()

		if s.server == nil {
			return
		}

		ctxShutdown, cancel := context.WithTimeout(ctx, constants.DefaultShutdownTimeout)
		defer cancel()

		shutdownErr = s.server.Shutdown(ctxShutdown)
		s.server = nil
	})

	if shutdownErr != nil {
		return ewrap.Wrap(shutdownErr, "shutdown diagnostics server")
	}

	return nil
}

// HandleStatus serves the /observe/status endpoint with a JSON snapshot of the runtime status.
func (s *Server) HandleStatus(w http.ResponseWriter, r *http.Request) {
	if s.cfg.AuthToken != "" {
		if !validAuth(r.Header.Get("Authorization"), s.cfg.AuthToken) {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}
	}

	snapshot := s.provider.Snapshot()
	snapshot.Timestamp = time.Now().UTC()

	w.Header().Set("Content-Type", "application/json")

	err := json.NewEncoder(w).Encode(snapshot)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func validAuth(header, token string) bool {
	const prefix = "Bearer "

	if header == "" {
		return false
	}

	if !strings.HasPrefix(header, prefix) {
		return false
	}

	return strings.TrimSpace(header[len(prefix):]) == token
}
