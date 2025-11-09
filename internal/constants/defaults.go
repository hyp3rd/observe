// Package constants provides common constants used across the observe project.
package constants

import "time"

const (
	// DefaultTimeout is the default timeout for requests.
	DefaultTimeout = 5 * time.Second
	// DefaultShutdownTimeout is the default timeout for shutdown operations.
	DefaultShutdownTimeout = 30 * time.Second
	// DefaultRetryAttempts is the default number of retry attempts for operations.
	DefaultRetryAttempts = 5
	// DefaultRetryDelay is the default delay between retry attempts.
	DefaultRetryDelay = 2 * time.Second
)
