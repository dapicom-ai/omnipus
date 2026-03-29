// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package credentials

import (
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/term"
)

const (
	// EnvMasterKey is the env var for a hex-encoded 256-bit master key.
	// When set, the key is used directly (no Argon2id KDF).
	EnvMasterKey = "OMNIPUS_MASTER_KEY"

	// EnvKeyFile is the env var for a path to a file containing the hex master key.
	// The file must have mode 0600 (owner read/write only).
	EnvKeyFile = "OMNIPUS_KEY_FILE"
)

// Unlock attempts to unlock store using the following provisioning priority:
//
//  1. OMNIPUS_MASTER_KEY environment variable (hex-encoded 256-bit key)
//  2. OMNIPUS_KEY_FILE environment variable (path to a 0600 file with hex key)
//  3. Interactive passphrase prompt (requires TTY; derives key via Argon2id)
//
// If no TTY is available and no env vars are set, the store remains locked and
// a warning is logged. Callers must check store.IsLocked() before use.
//
// Implements US-4 acceptance criteria.
func Unlock(store *Store) error {
	// Mode 1: OMNIPUS_MASTER_KEY direct hex key.
	if hexKey := os.Getenv(EnvMasterKey); hexKey != "" {
		key, err := hexToKey(hexKey)
		if err != nil {
			return fmt.Errorf("credentials: %w", err)
		}
		if err := store.UnlockWithKey(key); err != nil {
			return err
		}
		slog.Debug("credentials: unlocked via OMNIPUS_MASTER_KEY")
		return nil
	}

	// Mode 2: OMNIPUS_KEY_FILE — explicit path means failure is fatal (headless deployments
	// cannot fall back to an interactive prompt; silently continuing would cause a hang).
	if keyFile := os.Getenv(EnvKeyFile); keyFile != "" {
		key, err := loadKeyFile(keyFile)
		if err != nil {
			return fmt.Errorf("credentials: OMNIPUS_KEY_FILE %q failed: %w", keyFile, err)
		}
		if err := store.UnlockWithKey(key); err != nil {
			return err
		}
		slog.Debug("credentials: unlocked via OMNIPUS_KEY_FILE", "path", keyFile)
		return nil
	}

	// Mode 3: Interactive passphrase (requires TTY). Return an error when no TTY is
	// available — a silent nil would leave the store locked and cause confusing downstream
	// failures. Callers that allow a locked store should check before calling Unlock.
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("credentials: no master key available and no TTY — " +
			"set OMNIPUS_MASTER_KEY or OMNIPUS_KEY_FILE for headless operation")
	}

	passphrase, err := promptPassphrase("Enter Omnipus master passphrase: ")
	if err != nil {
		return fmt.Errorf("credentials: passphrase prompt: %w", err)
	}
	if passphrase == "" {
		return fmt.Errorf("credentials: passphrase must not be empty")
	}

	slog.Info("credentials: deriving key from passphrase (Argon2id, ~2s)...")
	if err := store.UnlockWithPassphrase(passphrase); err != nil {
		return err
	}
	slog.Debug("credentials: unlocked via interactive passphrase")
	return nil
}

// DeriveKeyFromPassphrase derives a 32-byte AES-256 key from passphrase + salt
// using Argon2id with parameters per SEC-23b.
func DeriveKeyFromPassphrase(passphrase string, salt []byte) []byte {
	return argon2.IDKey([]byte(passphrase), salt, argonTime, argonMemory, argonThreads, keyLen)
}

// hexToKey decodes a 64-character hex string into a 32-byte key.
func hexToKey(hexKey string) ([]byte, error) {
	if len(hexKey) != 64 {
		return nil, fmt.Errorf("invalid master key: expected 64 hex characters (256 bits), got %d", len(hexKey))
	}
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("invalid master key: not valid hex: %w", err)
	}
	return key, nil
}

// loadKeyFile reads a hex master key from path, enforcing 0600 permissions.
func loadKeyFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("key file stat %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("key file %q is not a regular file", path)
	}
	// Reject group- or world-readable files (US-4 AC3): mask 0o044 = group-read | world-read.
	if info.Mode()&0o044 != 0 {
		return nil, fmt.Errorf("key file %q has unsafe permissions %04o — must be 0600", path, info.Mode().Perm())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read key file %q: %w", path, err)
	}

	hexKey := strings.TrimRight(string(data), "\r\n")
	return hexToKey(hexKey)
}

// promptPassphrase reads a passphrase from the terminal without echo.
func promptPassphrase(prompt string) (string, error) {
	fd := int(os.Stdin.Fd())
	fmt.Fprint(os.Stderr, prompt)
	raw, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr) // newline after silent input
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// PromptNewPassphrase prompts for a new passphrase with confirmation.
func PromptNewPassphrase() (string, error) {
	pass1, err := promptPassphrase("New passphrase: ")
	if err != nil {
		return "", err
	}
	if pass1 == "" {
		return "", fmt.Errorf("credentials: passphrase must not be empty")
	}
	pass2, err := promptPassphrase("Confirm passphrase: ")
	if err != nil {
		return "", err
	}
	if pass1 != pass2 {
		return "", fmt.Errorf("credentials: passphrases do not match")
	}
	return pass1, nil
}
