//go:build !nogodmode

package sandbox

// GodModeAvailable reports whether the build supports the "off" sandbox
// profile (god mode). Compiled out via the `nogodmode` build tag for
// hosted/SaaS variants where disabling the sandbox is never permitted.
const GodModeAvailable = true
