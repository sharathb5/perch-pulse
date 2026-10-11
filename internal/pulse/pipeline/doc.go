// Package pipeline provides a small deterministic harness that wires the real
// Pulse packages end to end:
//
//	telemetry samples → detector → findings → incident assembly → file stores → API
//
// It is intentionally thin: no second detector implementation and no mocked
// incident payloads. Production Run Pulse may later call the same helpers;
// until then this package exists so CI can prove the integrated path without Docker.
package pipeline
