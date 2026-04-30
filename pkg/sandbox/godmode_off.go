//go:build nogodmode

package sandbox

// GodModeAvailable is false in builds compiled with the `nogodmode` tag.
// SaaS variants use this tag to ensure sandbox_profile=off can never be
// set, regardless of runtime flags.
const GodModeAvailable = false
