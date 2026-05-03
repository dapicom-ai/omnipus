// Package audit implements structured security audit logging for Omnipus.
//
// It provides SEC-15 (structured audit logging), SEC-16 (log redaction),
// and SEC-17 (explainable policy decisions) from the Omnipus BRD.
//
// Audit events are written as JSONL to ~/.omnipus/system/audit.jsonl.
// The logger rotates files daily or at the configured max size, retains for
// a configurable number of days, and recovers from corruption on startup.
package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Event types for audit logging.
const (
	EventToolCall   = "tool_call"
	EventExec       = "exec"
	EventFileOp     = "file_op"
	EventLLMCall    = "llm_call"
	EventPolicyEval = "policy_eval"
	EventRateLimit  = "rate_limit"
	EventSSRF       = "ssrf"
	EventStartup    = "startup"
	EventShutdown   = "shutdown"
)

// Decision values for audit entries.
const (
	DecisionAllow = "allow"
	DecisionDeny  = "deny"
	DecisionError = "error"
)

// Entry is a single audit log record.
type Entry struct {
	Timestamp  time.Time      `json:"timestamp"`
	Event      string         `json:"event"`
	Decision   string         `json:"decision,omitempty"`
	AgentID    string         `json:"agent_id,omitempty"`
	SessionID  string         `json:"session_id,omitempty"`
	Tool       string         `json:"tool,omitempty"`
	Command    string         `json:"command,omitempty"`
	Parameters map[string]any `json:"parameters,omitempty"`
	PolicyRule string         `json:"policy_rule,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

// LoggerConfig configures the audit logger.
type LoggerConfig struct {
	Dir            string   // Directory for audit files
	MaxSizeBytes   int64    // File rotation threshold (default 50MB)
	RetentionDays  int      // Days to retain rotated files (default 90)
	RedactPatterns []string // Custom redaction patterns
	RedactEnabled  bool     // Enable redaction
}

const defaultMaxSize = 50 * 1024 * 1024 // 50MB

// Logger writes audit entries as JSONL with rotation and retention.
type Logger struct {
	mu          sync.Mutex
	dir         string
	file        *os.File
	writer      *bufio.Writer
	currentSize int64
	currentDate string
	maxSize     int64
	retDays     int
	redactor    *Redactor
	degraded    bool
}

// LoggerConstructionError wraps an audit.NewLogger failure so callers can
// distinguish "audit logger could not be built" from generic boot errors.
//
// B1.2(b): when cfg.Sandbox.AuditLog == true and audit construction fails,
// the gateway must fail closed (treat as a sandbox boot error). Wrapping the
// underlying error in this typed sentinel lets the gateway recognise the
// case without string-matching the message and without importing internal
// state from the agent package.
type LoggerConstructionError struct {
	Dir string
	Err error
}

// Error makes LoggerConstructionError satisfy the error interface.
func (e *LoggerConstructionError) Error() string {
	if e == nil {
		return "audit: logger construction failed"
	}
	if e.Err == nil {
		return fmt.Sprintf("audit: logger construction failed for dir %q", e.Dir)
	}
	return fmt.Sprintf(
		"audit log construction failed for dir %q: %v; "+
			"either disable `sandbox.audit_log` or fix the underlying problem (permissions, disk, redaction patterns)",
		e.Dir, e.Err,
	)
}

// Unwrap exposes the underlying error for errors.Is/As traversal.
func (e *LoggerConstructionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// NewLogger creates an audit logger. It recovers from corruption on startup
// and runs retention cleanup.
func NewLogger(cfg LoggerConfig) (*Logger, error) {
	if cfg.MaxSizeBytes <= 0 {
		cfg.MaxSizeBytes = defaultMaxSize
	}
	if cfg.RetentionDays <= 0 {
		cfg.RetentionDays = 90
	}
	if err := os.MkdirAll(cfg.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("audit: cannot create directory %s: %w", cfg.Dir, err)
	}

	var redactor *Redactor
	if cfg.RedactEnabled {
		var err error
		redactor, err = NewRedactor(cfg.RedactPatterns)
		if err != nil {
			return nil, fmt.Errorf("audit: invalid redaction pattern: %w", err)
		}
	}

	l := &Logger{
		dir:      cfg.Dir,
		maxSize:  cfg.MaxSizeBytes,
		retDays:  cfg.RetentionDays,
		redactor: redactor,
	}

	l.recoverCorruption()

	if err := l.openCurrentFile(); err != nil {
		slog.Error("Audit log file cannot be opened. Operating in degraded mode.",
			"error", err, "path", cfg.Dir)
		l.degraded = true
	}

	// Run retention cleanup on startup
	l.cleanupExpired()

	return l, nil
}

// Log writes an audit entry. Returns an error on write failure but never panics.
//
// B1.2(a): Logger.Log is nil-safe. If the receiver is nil (e.g. when audit
// logger construction failed during boot but the rest of the system continues
// — see B1.2(b) for the fail-closed path), Log returns nil without panicking.
// This guard exists because the audit logger is reached through deeply-nested
// call chains (egress proxy denials, per-thread restrict failures, web_serve
// audit fail-closed) where adding a defensive nil-check at every call site
// would be brittle. Keeping the guard on the receiver makes every caller safe.
func (l *Logger) Log(entry *Entry) error {
	if l == nil {
		return nil
	}
	if entry == nil {
		return nil
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}

	// Apply redaction (SEC-16)
	if l.redactor != nil {
		l.redactEntry(entry)
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("audit: marshal failed: %w", err)
	}

	return l.writeLine(data)
}

// writeLine appends a single pre-marshaled JSON object as a JSONL record to the
// audit file, performing rotation and degraded-mode guarding identically to Log.
// It is reused by helpers that emit non-Entry-shaped records (e.g. security
// setting changes with flat top-level fields like actor/resource/old_value).
// The caller is responsible for any redaction before marshaling.
func (l *Logger) writeLine(data []byte) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.degraded || l.file == nil {
		slog.Error("Audit log entry written in degraded mode", "entry", string(data))
		return fmt.Errorf("audit: operating in degraded mode")
	}

	today := time.Now().UTC().Format("2006-01-02")
	if today != l.currentDate || l.currentSize >= l.maxSize {
		if rotateErr := l.rotate(); rotateErr != nil {
			slog.Error("Audit: rotation failed, entering degraded mode", "error", rotateErr)
			l.degraded = true
			return fmt.Errorf("audit: rotation failed: %w", rotateErr)
		}
	}

	line := append(data, '\n')
	n, err := l.writer.Write(line)
	if err != nil {
		l.degraded = true
		slog.Error("Audit log write failed, entering degraded mode", "error", err, "entry", string(data))
		return fmt.Errorf("audit: write failed: %w", err)
	}
	if err := l.writer.Flush(); err != nil {
		return fmt.Errorf("audit: flush failed: %w", err)
	}
	l.currentSize += int64(n)
	return nil
}

// Close flushes and closes the audit log file.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.writer != nil {
		if err := l.writer.Flush(); err != nil {
			if l.file != nil {
				l.file.Close()
			}
			return fmt.Errorf("audit: flush on close failed: %w", err)
		}
	}
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// RunRetentionCleanup deletes rotated audit files older than the retention period.
func (l *Logger) RunRetentionCleanup() error {
	l.cleanupExpired()
	return nil
}

func (l *Logger) auditPath() string {
	return filepath.Join(l.dir, "audit.jsonl")
}

func (l *Logger) openCurrentFile() error {
	path := l.auditPath()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("audit: open %s: %w", path, err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("audit: stat %s: %w", path, err)
	}
	l.file = f
	l.writer = bufio.NewWriter(f)
	l.currentSize = info.Size()
	l.currentDate = time.Now().UTC().Format("2006-01-02")
	l.degraded = false
	return nil
}

func (l *Logger) rotate() error {
	if l.writer != nil {
		l.writer.Flush()
	}
	if l.file != nil {
		l.file.Close()
	}

	src := l.auditPath()
	dst := filepath.Join(l.dir, fmt.Sprintf("audit-%s.jsonl", l.currentDate))
	if _, err := os.Stat(dst); err == nil {
		dst = filepath.Join(l.dir, fmt.Sprintf("audit-%s-%d.jsonl",
			l.currentDate, time.Now().UnixMilli()))
	}

	if err := os.Rename(src, dst); err != nil {
		return l.openCurrentFile()
	}

	slog.Info("Audit log rotated", "to", dst)
	return l.openCurrentFile()
}

func (l *Logger) recoverCorruption() {
	path := l.auditPath()
	f, err := os.Open(path)
	if err != nil {
		slog.Warn("audit: could not open file for corruption recovery", "path", path, "error", err)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		if err != nil {
			slog.Warn("audit: could not stat file for corruption recovery", "path", path, "error", err)
		}
		return
	}

	lastLine, lastLineStart := readLastLine(f, info.Size())
	if lastLine == "" {
		return
	}

	var js json.RawMessage
	if json.Unmarshal([]byte(lastLine), &js) == nil {
		return
	}

	slog.Warn("Audit log: truncating malformed last line", "path", path, "truncate_at", lastLineStart)
	f.Close()
	os.Truncate(path, lastLineStart)
}

func readLastLine(r io.ReadSeeker, size int64) (string, int64) {
	bufSize := int64(4096)
	if bufSize > size {
		bufSize = size
	}
	offset := size - bufSize
	r.Seek(offset, io.SeekStart)

	buf := make([]byte, bufSize)
	n, err := io.ReadFull(r, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return "", size
	}
	buf = buf[:n]

	lines := strings.Split(string(buf), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			lineStart := offset
			for j := 0; j < i; j++ {
				lineStart += int64(len(lines[j])) + 1
			}
			return line, lineStart
		}
	}
	return "", size
}

func (l *Logger) cleanupExpired() {
	cutoff := time.Now().UTC().AddDate(0, 0, -l.retDays)
	pattern := filepath.Join(l.dir, "audit-*.jsonl")
	matches, _ := filepath.Glob(pattern)
	sort.Strings(matches)

	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(path); err != nil {
				slog.Error("Audit: failed to remove expired log", "path", path, "error", err)
			} else {
				slog.Info("Audit: removed expired log", "path", path)
			}
		}
	}
}

func (l *Logger) redactEntry(entry *Entry) {
	if l.redactor == nil {
		return
	}
	if entry.Parameters != nil {
		entry.Parameters = l.redactor.redactMap(entry.Parameters)
	}
	if entry.Details != nil {
		entry.Details = l.redactor.redactMap(entry.Details)
	}
	entry.Command = l.redactor.Redact(entry.Command)
}
