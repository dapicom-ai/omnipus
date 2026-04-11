// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 Omnipus contributors

package config

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// This file provides:
//   - SensitiveDataCache: runtime lookup for filtering credential values out of
//     LLM responses and logs (used throughout the agent loop)
//   - SecureString / SecureStrings: typed wrappers for credential values that
//     are redacted on JSON serialization
//   - SecureModelList: type alias for []*ModelConfig used by the Providers field
//
// The old PicoClaw ".security.yml" credential-separation mechanism has been
// removed. Credential separation is now handled exclusively by the Omnipus
// encrypted credentials.json store (pkg/credentials) referenced via
// ModelConfig.APIKeyRef — see pkg/gateway/rest.go and pkg/sysagent/tools/deps.go
// for the resolution path. The credentials.json store uses AES-256-GCM +
// Argon2id per CLAUDE.md and BRD SEC-23.

// SensitiveDataCache caches the compiled strings.Replacer for filtering
// sensitive data out of LLM responses and logs.
//
// Computed once on first access via sync.Once.
type SensitiveDataCache struct {
	replacer *strings.Replacer
	once     sync.Once
}

// SensitiveDataReplacer returns the strings.Replacer for filtering sensitive data.
// It is computed once on first access via sync.Once.
func (sec *Config) SensitiveDataReplacer() *strings.Replacer {
	sec.initSensitiveCache()
	return sec.sensitiveCache.replacer
}

// initSensitiveCache initializes the sensitive data cache if not already done.
func (sec *Config) initSensitiveCache() {
	if sec.sensitiveCache == nil {
		sec.sensitiveCache = &SensitiveDataCache{}
	}
	sec.sensitiveCache.once.Do(func() {
		values := sec.collectSensitiveValues()
		if len(values) == 0 {
			sec.sensitiveCache.replacer = strings.NewReplacer()
			return
		}

		// Build old/new pairs for strings.Replacer
		var pairs []string
		for _, v := range values {
			if len(v) > 3 {
				pairs = append(pairs, v, "[FILTERED]")
			}
		}
		if len(pairs) == 0 {
			sec.sensitiveCache.replacer = strings.NewReplacer()
			return
		}
		sec.sensitiveCache.replacer = strings.NewReplacer(pairs...)
	})
}

// collectSensitiveValues collects all sensitive strings from Config using reflection.
func (sec *Config) collectSensitiveValues() []string {
	var values []string
	collectSensitive(reflect.ValueOf(sec), &values)
	return values
}

// collectSensitive recursively traverses the value and collects SecureString/SecureStrings values.
func collectSensitive(v reflect.Value, values *[]string) {
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}

	t := v.Type()

	// SecureString: collect via String() method (defined on *SecureString)
	if t == reflect.TypeOf(SecureString{}) {
		var addr reflect.Value
		if v.CanAddr() {
			addr = v.Addr()
		} else {
			tmp := reflect.New(t)
			tmp.Elem().Set(v)
			addr = tmp
		}
		ss, ok := addr.Interface().(*SecureString)
		if ok && ss != nil {
			s := ss.String()
			if s != "" {
				*values = append(*values, s)
			}
		}
		return
	}

	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			collectSensitive(v.Field(i), values)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			collectSensitive(v.Index(i), values)
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			collectSensitive(iter.Value(), values)
		}
	}
}

const (
	notHere = `"[NOT_HERE]"`
)

// SecureStrings is a slice of SecureString.
type SecureStrings []*SecureString

// Values returns the resolved values.
func (s *SecureStrings) Values() []string {
	if s == nil {
		return nil
	}
	keys := make([]string, len(*s))
	for i, k := range *s {
		keys[i] = k.String()
	}
	return unique(keys)
}

// SimpleSecureStrings builds a SecureStrings from plain string values.
// Used by tests and by internal config construction.
func SimpleSecureStrings(val ...string) SecureStrings {
	val = unique(val)
	vv := make(SecureStrings, len(val))
	for i, s := range val {
		vv[i] = NewSecureString(s)
	}
	return vv
}

// unique returns a new slice with duplicate elements removed.
func unique[T comparable](input []T) []T {
	m := make(map[T]struct{})
	var result []T
	for _, v := range input {
		if _, ok := m[v]; !ok {
			m[v] = struct{}{}
			result = append(result, v)
		}
	}
	return result
}

func (s SecureStrings) MarshalJSON() ([]byte, error) {
	return []byte(notHere), nil
}

func (s *SecureStrings) UnmarshalJSON(value []byte) error {
	if string(value) == notHere {
		return nil
	}
	var v []*SecureString
	err := json.Unmarshal(value, &v)
	if err != nil {
		return err
	}
	*s = v
	return nil
}

// SecureString wraps a credential value. It is redacted on JSON output
// (MarshalJSON returns "[NOT_HERE]") so secrets never leak into API responses
// or logged config. On UnmarshalJSON it reads the plaintext value verbatim.
//
// Callers that need to store credentials separately should use the encrypted
// credentials.json store via ModelConfig.APIKeyRef instead of putting secrets
// directly in config.json.
//
//nolint:recvcheck
type SecureString struct {
	resolved string
}

// IsZero reports whether this SecureString has no value. Used by JSON
// `omitzero` to skip empty fields in serialized output.
func (s SecureString) IsZero() bool {
	return s.resolved == ""
}

// NewSecureString constructs a SecureString from a plaintext value.
func NewSecureString(value string) *SecureString {
	return &SecureString{resolved: value}
}

// String returns the plaintext value.
func (s *SecureString) String() string {
	if s == nil {
		return ""
	}
	return s.resolved
}

// Set replaces the stored value with a new plaintext.
func (s *SecureString) Set(value string) *SecureString {
	s.resolved = value
	return s
}

// MarshalJSON redacts the value in JSON output. The credential should NEVER
// be serialized to JSON — use credentials.json + APIKeyRef for persistence.
func (s SecureString) MarshalJSON() ([]byte, error) {
	return []byte(notHere), nil
}

func (s *SecureString) UnmarshalJSON(value []byte) error {
	if string(value) == notHere {
		return nil
	}
	var v string
	if err := json.Unmarshal(value, &v); err != nil {
		return err
	}
	s.resolved = v
	return nil
}

func (s SecureString) MarshalYAML() (any, error) {
	return s.resolved, nil
}

func (s *SecureString) UnmarshalYAML(value *yaml.Node) error {
	s.resolved = value.Value
	return nil
}

func (s *SecureString) UnmarshalText(text []byte) error {
	s.resolved = string(text)
	return nil
}

// SecureModelList is the type used by Config.Providers. It is a plain slice of
// *ModelConfig; the credential-separation custom UnmarshalYAML that used to
// live here has been removed along with the .security.yml mechanism.
type SecureModelList []*ModelConfig
