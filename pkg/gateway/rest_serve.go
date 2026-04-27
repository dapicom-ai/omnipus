//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

// Package gateway — /serve/{agent}/{token}/... static-asset handler for
// the chat-served-iframe-preview feature (chat-served-iframe-preview-spec.md).
//
// Auth model (FR-023): TOKEN-ONLY. The path token IS the credential.
// There is no session-cookie or bearer-header check; the registration in
// `servedSubdirs` is mintable only by an authenticated agent loop, so the
// token is a post-authorisation artefact. This route is registered on the
// PREVIEW listener (Track A) which does not register /api/v1/* paths, so
// served JS cannot reach the SPA's API surface even with a same-origin
// fetch (FR-005 / Threat Model T-02).
//
// The handler enforces:
//   - Method gate (GET/HEAD allowed; OPTIONS handled separately for CORS).
//   - Token presence + agent match (T-09 documented: anyone with the URL
//     has access — bearer-token contract).
//   - 401 on expired/unknown token (FR-018 — preserves existing wording).
//   - 403 on token-agent mismatch (defence-in-depth).
//
// Response headers (FR-007 / FR-007b / FR-007c):
//   - Content-Security-Policy: frame-ancestors '<main_origin>' (or '*'
//     fallback per FR-007e) plus connect-src 'self' + form-action 'self'.
//   - Referrer-Policy: no-referrer (T-03 — token must not leak via Referer).
//   - X-Content-Type-Options: nosniff.
//
// CORS preflight (FR-007a): OPTIONS requests with Origin matching the
// configured main origin receive 204 with allow headers; foreign origins
// receive 204 without allow headers (browser blocks). The SPA's warmup
// path uses iframe-load polling, NOT cross-origin fetch, so CORS is for
// operator tooling (curl, monitoring probes) only.
//
// Audit (FR-024 / FR-024a):
//   - serve.served (Info) on the FIRST 200-response per token within its
//     TTL — subsequent requests on the same token are NOT logged (a
//     hydrated SPA can fire 50+ asset requests per page load and per-asset
//     audit floods the bus).
//   - serve.token_invalid (Warn, decision: deny) on 401.
//   - serve.token_agent_mismatch (Warn, decision: deny) on 403.
//   - serve.path_invalid (Warn, decision: error) on 404.
//   - serve.malformed_url (Warn, decision: error) on 400.
//
// Threat Model T-05 — localStorage sharing between tabs:
// Two /serve/<agent>/<token>/ instances opened in two browser tabs share
// localStorage because both are served from the same preview origin.  This
// is an accepted single-tenant limitation documented in the spec
// (docs/specs/chat-served-iframe-preview-spec.md, Threat Model T-05).  The
// future-hardening path — per-token subdomain isolation — is described in
// the spec's deferred work section.  Current mitigation: served JS has no
// cross-origin access to the SPA's main-listener localStorage (enforced by
// the two-port topology; Threat Model T-01).

package gateway

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// HandleServeWorkspace serves GET /serve/{agent_id}/{token}/{file_path...}
// on the PREVIEW listener (Track A registration; this handler is mounted
// without RequireSessionCookieOrBearer per FR-023).
func (a *restAPI) HandleServeWorkspace(w http.ResponseWriter, r *http.Request) {
	// CORS preflight (FR-007a). Handled before any other gating because
	// preflights MUST NOT carry credentials and bypass the method gate.
	if r.Method == http.MethodOptions {
		a.handleServePreviewPreflight(w, r)
		return
	}

	startedAt := time.Now()

	// Method gate: GET (and HEAD by extension) only. Static serving is
	// inherently read-only; other verbs would create a CSRF surface on
	// any reverse proxy in front.
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		jsonErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Parse /serve/{agent_id}/{token}/{file_path...}
	remainder := strings.TrimPrefix(r.URL.Path, "/serve/")
	// F-47: explicit double-slash detection. A request to "/serve//token/file"
	// would otherwise have its leading "/" silently trimmed below, producing
	// agentID=token, token=file, and (for any non-registered "file" token) a
	// confusing 401. The correct behaviour is 400 + serve.malformed_url so
	// operators see a clear "this URL is not parseable" signal rather than a
	// misleading auth failure. We check BEFORE the leading-slash trim.
	if strings.HasPrefix(remainder, "/") {
		a.auditServeFailure(r, "serve.malformed_url", "error", "", "", http.StatusBadRequest, startedAt)
		jsonErr(w, http.StatusBadRequest, "malformed serve URL")
		return
	}

	parts := strings.SplitN(remainder, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		a.auditServeFailure(r, "serve.malformed_url", "error", "", "", http.StatusBadRequest, startedAt)
		jsonErr(w, http.StatusBadRequest, "malformed URL: expected /serve/{agent}/{token}/...")
		return
	}

	agentID := parts[0]
	token := parts[1]
	var relPath string
	if len(parts) == 3 {
		relPath = parts[2]
	}

	if err := validateEntityID(agentID); err != nil {
		a.auditServeFailure(r, "serve.malformed_url", "error", agentID, token, http.StatusBadRequest, startedAt)
		jsonErr(w, http.StatusBadRequest, "invalid agent ID")
		return
	}

	// Validate the token against the registry. Unknown or expired → 401
	// (FR-018: preserve existing 401 wording — NOT 410).
	if a.servedSubdirs == nil {
		jsonErr(w, http.StatusInternalServerError, "serve registry not initialised")
		return
	}
	entry := a.servedSubdirs.Lookup(token)
	if entry == nil {
		a.auditServeFailure(r, "serve.token_invalid", "deny", agentID, token, http.StatusUnauthorized, startedAt)
		jsonErr(w, http.StatusUnauthorized, "token unknown or expired")
		return
	}

	// Cross-agent token check: the token must belong to the agent in the URL.
	// Defence-in-depth — without this a leaked token issued for agent B
	// would allow access to agent A's served subtree if both happened to
	// match the URL pattern.
	if entry.AgentID != agentID {
		a.auditServeFailure(r, "serve.token_agent_mismatch", "deny", agentID, token, http.StatusForbidden, startedAt)
		jsonErr(w, http.StatusForbidden, "token does not belong to this agent")
		return
	}

	// FR-023: NO session-cookie or ownership check. The token is the
	// credential. Owner enforcement happened at token issuance time
	// (only the owner of an agent can call serve_workspace for that
	// agent); the token is the post-authorisation artefact.

	// Resolve current config (used for resolveMainOrigin → CSP).
	cfg := configFromContext(r.Context())
	if cfg == nil {
		cfg = a.agentLoop.GetConfig()
	}
	mainOrigin := resolveMainOrigin(cfg)

	// Resolve the file path within the registered directory.
	// entry.AbsDir is already canonicalised by the serve_workspace tool.
	// Use filepath.Join + canonicalisation to prevent path traversal.
	var absPath string
	if relPath == "" || relPath == "." {
		absPath = entry.AbsDir
	} else {
		candidate := filepath.Join(entry.AbsDir, filepath.FromSlash(relPath))
		// Verify the candidate stays within the registered directory.
		dirWithSep := entry.AbsDir
		if !strings.HasSuffix(dirWithSep, string(filepath.Separator)) {
			dirWithSep += string(filepath.Separator)
		}
		cleaned := filepath.Clean(candidate)
		if cleaned != entry.AbsDir && !strings.HasPrefix(cleaned, dirWithSep) {
			a.auditServeFailure(r, "serve.path_invalid", "error", agentID, token, http.StatusForbidden, startedAt)
			jsonErr(w, http.StatusForbidden, "access denied: path is outside the registered directory")
			return
		}
		// F-28: symlink-escape defence. The lexical prefix check above only
		// validates the textual path. os.Stat below follows symlinks, so a
		// symlink inside entry.AbsDir pointing to /etc/passwd would pass the
		// prefix check on the unresolved path and then be silently read.
		// Resolve symlinks here and re-verify the resolved target stays
		// within entry.AbsDir. EvalSymlinks failing with anything other
		// than "does not exist" is treated as access denied; "does not
		// exist" is left to the os.Stat 404 path below.
		if resolved, evalErr := filepath.EvalSymlinks(cleaned); evalErr == nil {
			if resolved != entry.AbsDir && !strings.HasPrefix(resolved, dirWithSep) {
				a.auditServeFailure(r, "serve.path_invalid", "error", agentID, token, http.StatusForbidden, startedAt)
				jsonErr(w, http.StatusForbidden, "access denied: path is outside the registered directory")
				return
			}
			cleaned = resolved
		} else if !os.IsNotExist(evalErr) {
			a.auditServeFailure(r, "serve.path_invalid", "error", agentID, token, http.StatusForbidden, startedAt)
			jsonErr(w, http.StatusForbidden, "access denied: path could not be resolved")
			return
		}
		absPath = cleaned
	}

	// Stat the target.
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			a.auditServeFailure(r, "serve.path_invalid", "error", agentID, token, http.StatusNotFound, startedAt)
			jsonErr(w, http.StatusNotFound, "file not found")
			return
		}
		slog.Error("rest: HandleServeWorkspace: stat failed", "path", absPath, "error", err)
		jsonErr(w, http.StatusInternalServerError, "could not stat path")
		return
	}

	// If the path is a directory, look for index.html.
	if info.IsDir() {
		indexPath := filepath.Join(absPath, "index.html")
		indexInfo, indexErr := os.Stat(indexPath)
		if indexErr != nil || indexInfo.IsDir() {
			a.auditServeFailure(r, "serve.path_invalid", "error", agentID, token, http.StatusNotFound, startedAt)
			jsonErr(w, http.StatusNotFound, "no index.html in directory")
			return
		}
		absPath = indexPath
		info = indexInfo
	}

	// FR-024: emit serve.served on the first request per token only.
	// firstServedTokens (rest_preview_audit.go) tracks tokens that have
	// already been audited; later requests within the token's TTL produce
	// no audit entry to avoid flooding the bus with per-asset events
	// (a hydrated SPA can fire 50+ asset requests per page load).
	emitFirstServed := a.markFirstServed(token)

	if info.Size() <= workspaceStreamingThreshold {
		// Buffered path: read into memory before setting any headers so that
		// read errors can still return a proper HTTP error status.
		data, readErr := os.ReadFile(absPath)
		if readErr != nil {
			slog.Error("rest: HandleServeWorkspace: ReadFile failed", "path", absPath, "error", readErr)
			jsonErr(w, http.StatusInternalServerError, "could not read file")
			return
		}
		// Headers set only after successful read — status code is still settable.
		setWorkspaceSecurityHeaders(w, mainOrigin)
		w.Header().Set("Content-Type", contentTypeForPath(absPath))
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			if _, writeErr := w.Write(data); writeErr != nil {
				slog.Debug("rest: HandleServeWorkspace: write failed", "error", writeErr)
			}
		}
		if emitFirstServed {
			a.auditServeSuccess(r, "serve.served", agentID, token, http.StatusOK, startedAt, int64(len(data)))
		}
		return
	}

	// Streaming path: open file BEFORE setting any response headers so that an
	// open failure can still return a proper HTTP error status (not a silent 200
	// with an empty body). Once WriteHeader is called the status code is frozen.
	f, openErr := os.Open(absPath)
	if openErr != nil {
		slog.Error("rest: HandleServeWorkspace: Open failed", "path", absPath, "error", openErr)
		jsonErr(w, http.StatusInternalServerError, "could not open file")
		return
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			slog.Debug("rest: HandleServeWorkspace: file close error", "error", closeErr)
		}
	}()
	// File is open — no more error paths before the response. Set headers now.
	setWorkspaceSecurityHeaders(w, mainOrigin)
	w.Header().Set("Content-Type", contentTypeForPath(absPath))
	w.WriteHeader(http.StatusOK)
	var bytesOut int64
	if r.Method != http.MethodHead {
		var copyErr error
		bytesOut, copyErr = io.Copy(w, f)
		if copyErr != nil {
			slog.Debug("rest: HandleServeWorkspace: io.Copy failed", "error", copyErr)
		}
	}
	if emitFirstServed {
		a.auditServeSuccess(r, "serve.served", agentID, token, http.StatusOK, startedAt, bytesOut)
	}
}

// handleServePreviewPreflight handles CORS OPTIONS for /serve/ and /dev/
// (FR-007a). Same-main-origin requests get full allow headers; foreign
// origins get a 204 without Access-Control-Allow-Origin so the browser
// blocks the actual request.
func (a *restAPI) handleServePreviewPreflight(w http.ResponseWriter, r *http.Request) {
	cfg := configFromContext(r.Context())
	if cfg == nil {
		cfg = a.agentLoop.GetConfig()
	}
	mainOrigin := resolveMainOrigin(cfg)
	requestOrigin := r.Header.Get("Origin")

	// Vary: Origin so caches don't reuse a same-origin response for a
	// foreign-origin request.
	w.Header().Set("Vary", "Origin")
	if mainOrigin != "" && requestOrigin != "" && strings.EqualFold(
		strings.TrimRight(requestOrigin, "/"),
		strings.TrimRight(mainOrigin, "/"),
	) {
		w.Header().Set("Access-Control-Allow-Origin", mainOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
		w.Header().Set("Access-Control-Max-Age", "86400")
	}
	// Always return 204 — the caller's browser checks for the
	// Access-Control-Allow-Origin header and will block on its absence.
	// Returning 403 would risk leaking that the route exists; 204 with no
	// allow header is browser-blocking and consistent.
	w.WriteHeader(http.StatusNoContent)
}

// auditServeSuccess emits a serve.served (or dev.proxied) event at Info
// level for FR-024. Only the first request per token within the token's
// TTL reaches this code path (gated by ServedSubdirs.MarkServedOnce).
func (a *restAPI) auditServeSuccess(
	r *http.Request,
	event string,
	agentID, token string,
	status int,
	startedAt time.Time,
	bytesOut int64,
) {
	a.emitPreviewAuditEntry(r, event, "allow", agentID, token, status, startedAt, bytesOut, slog.LevelInfo)
}

// auditServeFailure emits a serve.* failure event (FR-024a). Decisions:
// "deny" for 401/403 (denied access), "error" for 400/404 (malformed
// input or not-found path traversal). All failures log at Warn so
// operators can see token-guessing probes.
func (a *restAPI) auditServeFailure(
	r *http.Request,
	event string,
	decision string,
	agentID, token string,
	status int,
	startedAt time.Time,
) {
	a.emitPreviewAuditEntry(r, event, decision, agentID, token, status, startedAt, 0, slog.LevelWarn)
}
