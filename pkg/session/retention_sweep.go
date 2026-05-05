package session

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RetentionSweep deletes .jsonl files inside session subdirectories whose
// mtime is older than retentionDays*24h. It returns the count of files deleted.
//
// When retentionDays <= 0 the method is a no-op and returns (0, nil).
// Per-file delete errors are logged at Warn and the sweep continues.
// An error is returned only if the base directory walk cannot start.
//
// After all aged .jsonl files are removed, session directories that contain
// only stale sidecar metadata (meta.json / .lock / .ulid) AND whose directory
// mtime is itself past the cutoff are removed entirely so ListSessions does
// not continue to surface a session with no transcript.
func (us *UnifiedStore) RetentionSweep(retentionDays int) (int, error) {
	if retentionDays <= 0 {
		return 0, nil
	}

	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	removed := 0
	// Track session dirs we touched so we can decide which to remove entirely.
	touchedSessionDirs := make(map[string]struct{})

	err := filepath.WalkDir(us.baseDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == us.baseDir {
				return walkErr
			}
			slog.Warn("session: retention_sweep: walk error", "path", path, "error", walkErr)
			return nil
		}

		if d.IsDir() {
			if path == us.baseDir {
				return nil
			}
			rel, err := filepath.Rel(us.baseDir, path)
			if err != nil {
				return err
			}
			parts := strings.SplitN(rel, string(filepath.Separator), 2)
			if len(parts) == 1 {
				name := parts[0]
				if name == ".context" {
					return filepath.SkipDir
				}
			}
			return nil
		}

		if !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}

		rel, err := filepath.Rel(us.baseDir, path)
		if err != nil {
			return err
		}
		parts := strings.SplitN(rel, string(filepath.Separator), 3)
		if len(parts) < 2 {
			return nil
		}
		if parts[0] == ".context" {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			slog.Warn("session: retention_sweep: stat failed", "file", path, "error", err)
			return nil
		}

		if info.ModTime().Before(cutoff) {
			if delErr := os.Remove(path); delErr != nil {
				slog.Warn("session: retention_sweep: delete failed", "file", path, "error", delErr)
			} else {
				removed++
				touchedSessionDirs[filepath.Join(us.baseDir, parts[0])] = struct{}{}
			}
		}

		return nil
	})
	if err != nil {
		return removed, err
	}

	// Second pass: remove session directories that lost all their transcripts
	// to the sweep AND whose remaining sidecar files are all themselves past
	// the cutoff. This prevents ListSessions from continuing to enumerate a
	// session whose data has been swept.
	for sessDir := range touchedSessionDirs {
		entries, readErr := os.ReadDir(sessDir)
		if readErr != nil {
			continue
		}
		stillFresh := false
		for _, ent := range entries {
			info, infoErr := ent.Info()
			if infoErr != nil {
				continue
			}
			if !info.ModTime().Before(cutoff) {
				stillFresh = true
				break
			}
		}
		if stillFresh {
			continue
		}
		if rmErr := os.RemoveAll(sessDir); rmErr != nil {
			slog.Warn("session: retention_sweep: dir remove failed", "dir", sessDir, "error", rmErr)
		}
	}

	return removed, nil
}
