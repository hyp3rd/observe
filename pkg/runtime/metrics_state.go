package runtime

import "sync/atomic"

// MetricsState tracks runtime-level counters that must persist across reloads.
type MetricsState struct {
	configReloads atomic.Int64
}

// NewMetricsState constructs an empty MetricsState.
func NewMetricsState() *MetricsState {
	return &MetricsState{}
}

// IncrementConfigReloads increments the config reload counter.
func (m *MetricsState) IncrementConfigReloads() {
	if m == nil {
		return
	}

	m.configReloads.Add(1)
}

// ConfigReloads returns the current number of config reloads recorded.
func (m *MetricsState) ConfigReloads() int64 {
	if m == nil {
		return 0
	}

	return m.configReloads.Load()
}
