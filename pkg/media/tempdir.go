package media

import (
	"os"
	"path/filepath"
)

const TempDirName = "omnipus_media"

// TempDir returns the directory used for downloaded media. When
// OMNIPUS_HOME is set the dir lives inside the workspace
// ($OMNIPUS_HOME/media) so files survive across gateway restarts and
// stay inside the Landlock-allowed paths. Falls back to
// $TMPDIR/omnipus_media for tests and ad-hoc invocations.
func TempDir() string {
	if home := os.Getenv("OMNIPUS_HOME"); home != "" {
		return filepath.Join(home, "media")
	}
	return filepath.Join(os.TempDir(), TempDirName)
}
