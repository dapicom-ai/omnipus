//go:build !cgo

package testutil

// cgoEnabled is false when the binary was compiled with CGO_ENABLED=0.
const cgoEnabled = false
