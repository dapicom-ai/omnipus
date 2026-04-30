//go:build !cgo

// Omnipus - Ultra-lightweight personal AI agent
// License: MIT
// Copyright (c) 2026 Omnipus contributors

// Package gateway — /dev/{agent}/{token}/... reverse proxy for Tier 3
// dev servers, refactored for the chat-served-iframe-preview feature
// (chat-served-iframe-preview-spec.md).
//
// Auth model (FR-023 / FR-023a): TOKEN-ONLY, NO origin check.
//
// Why no origin check (FR-023a / CR-02): the iframe runs at
// <preview_origin> and any form POST inside it carries
// "Origin: <preview_origin>" which never matches the SPA's main origin.
// The pre-fix RequireMatchingOriginOnStateChanging middleware rejected
// every legitimate iframe POST with 403, breaking dev-iframe form
// flows. The path token IS the credential; foreign-origin CSRF is
// blocked by the CSP `frame-ancestors '<main_origin>'` directive
// (Threat Model T-04).
//
// Why no session-cookie / ownership check (FR-023): the route is
// registered on the PREVIEW listener (Track A) without
// RequireSessionCookieOrBearer. Owner enforcement happened at token
// issuance time — only the owner of an agent can call run_in_workspace
// for that agent.
//
// Response headers (FR-007 / FR-007b / FR-007d):
//   - Content-Security-Policy: gateway's policy (frame-ancestors
//     '<main_origin>'; connect-src 'self'; ...) injected via
//     setWorkspaceSecurityHeaders. The reverse-proxy ModifyResponse hook
//     STRIPS any upstream CSP from the dev server first — Next.js / Vite
//     emit their own CSP with 'unsafe-eval' for HMR; if both reach the
//     browser the intersection breaks HMR.
//   - X-Frame-Options stripped from upstream for the same reason.
//   - Referrer-Policy: no-referrer (same helper as /serve/).
//
// CORS preflight (FR-007a): same handler as /serve/.
//
// Audit (FR-024 / FR-024a): dev.proxied at Info level on first request per
// token, dev.* failures at Warn. The FR-024 spec calls for Debug-default;
// the level was raised to Info because the audit primitive does not currently
// distinguish levels at the entry layer.

package gateway

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"runtime"
	"strings"
	"time"

	"github.com/dapicom-ai/omnipus/pkg/sandbox"
	"github.com/dapicom-ai/omnipus/pkg/tools"
	"github.com/dapicom-ai/omnipus/pkg/validation"
)

// devProxyPathPrefix is the URL prefix served by HandleDevProxy.
const devProxyPathPrefix = "/dev/"

// HandleDevProxy is the http.Handler entry point for /dev/{agent}/{token}/...
// requests. Token + agent_id check is the sole credential per FR-023.
//
// Wiring: registered by gateway.go on the PREVIEW listener WITHOUT
// RequireSessionCookieOrBearer. The Origin-matching middleware is
// dropped per FR-023a — see file header for rationale.
func (a *restAPI) HandleDevProxy(w http.ResponseWriter, r *http.Request) {
	// CORS preflight (FR-007a). Identical handling to /serve/ — both
	// routes share handleServePreviewPreflight.
	if r.Method == http.MethodOptions {
		a.handleServePreviewPreflight(w, r)
		return
	}

	startedAt := time.Now()

	// Linux gate. Spec v4 — Tier 3 is Linux-only. We send the
	// same wording the tool emits so SPA error handlers can pattern-match
	// once.
	if runtime.GOOS != "linux" {
		writeDevProxyError(w, http.StatusServiceUnavailable, tools.Tier3UnsupportedMessage)
		return
	}

	if a.devServers == nil {
		writeDevProxyError(w, http.StatusServiceUnavailable, "dev-server registry not configured")
		return
	}

	// Parse path: /dev/{agent_id}/{token}/{remaining...}
	rest := strings.TrimPrefix(r.URL.Path, devProxyPathPrefix)
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		a.auditDevFailure(r, "dev.malformed_url", "error", "", "", http.StatusBadRequest, startedAt)
		writeDevProxyError(w, http.StatusBadRequest, "invalid /dev path; expected /dev/<agent>/<token>/...")
		return
	}
	agentID := parts[0]
	token := parts[1]
	remaining := ""
	if len(parts) == 3 {
		remaining = parts[2]
	}

	// F-32: validate the agent identifier shape consistently with /serve/.
	// Without this an agentID containing "/", "..", or NUL would proceed
	// to the registry lookup (which uses token only) and silently match;
	// the resulting audit entries would carry a malformed agent_id that
	// pollutes operator queries. Mirror the /serve/ pattern (rest_serve.go).
	if err := validation.EntityID(agentID); err != nil {
		a.auditDevFailure(r, "dev.path_invalid", "error", agentID, "", http.StatusBadRequest, startedAt)
		writeDevProxyError(w, http.StatusBadRequest, "invalid agent identifier")
		return
	}

	// F-15: normalise the remaining path to prevent directory traversal.
	// path.Clean collapses ".." sequences, double slashes, and trailing
	// dots. After Clean, a valid path starts with "" (empty → serve root)
	// or a literal segment — it cannot start with ".." because Clean
	// resolves those. We check anyway as defence-in-depth.
	if remaining != "" {
		cleaned := path.Clean("/" + remaining)
		if strings.HasPrefix(cleaned, "/..") {
			a.auditDevFailure(r, "dev.path_traversal", "deny", agentID, token, http.StatusBadRequest, startedAt)
			writeDevProxyError(w, http.StatusBadRequest, "path traversal not allowed")
			return
		}
		// Strip the leading "/" added for Clean so we restore the relative form.
		remaining = strings.TrimPrefix(cleaned, "/")
	}

	// FR-023: NO session-cookie / ownership / Origin check. Token + agent
	// match are the sole credentials. Owner enforcement happened at token
	// issuance time (only the owner of an agent can call
	// run_in_workspace for that agent).

	// Token validity. Lookup also touches LastActivity (sliding idle
	// timer) so an active dev session keeps refreshing.
	reg := a.devServers.Lookup(token)
	if reg == nil {
		a.auditDevFailure(r, "dev.token_invalid", "deny", agentID, token, http.StatusServiceUnavailable, startedAt)
		writeDevProxyError(w, http.StatusServiceUnavailable, "dev-server registration not found or expired")
		return
	}
	// Defence-in-depth: token must belong to the agent in the URL.
	// Without this check a user with access to agent A could reuse a
	// token issued for agent B.
	if reg.AgentID != agentID {
		a.auditDevFailure(r, "dev.token_agent_mismatch", "deny", agentID, token, http.StatusForbidden, startedAt)
		writeDevProxyError(w, http.StatusForbidden, "token does not match agent")
		return
	}

	// Forward to the dev server. proxyDevRequest handles the upstream
	// CSP/XFO strip, the gateway-CSP injection, and audit emission on
	// the first served asset.
	a.proxyDevRequest(w, r, reg, remaining, agentID, token, startedAt)
}

// proxyDevRequest forwards the request to the dev-server's loopback port.
// Strips the /dev/<agent>/<token> prefix so the embedded app sees its own
// root paths. All HTTP methods are forwarded (dev apps need POSTs and
// websocket upgrades).
//
// FR-007d: rp.ModifyResponse strips upstream Content-Security-Policy
// and X-Frame-Options so the gateway-injected policy is authoritative.
// Next.js / Vite dev servers emit their own CSP including 'unsafe-eval'
// for HMR; if both reach the browser the intersection breaks HMR.
func (a *restAPI) proxyDevRequest(
	w http.ResponseWriter,
	r *http.Request,
	reg *sandbox.DevServerRegistration,
	remaining string,
	agentID, token string,
	startedAt time.Time,
) {
	target := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("127.0.0.1:%d", reg.Port),
	}
	rp := httputil.NewSingleHostReverseProxy(target)
	// F-29: route ReverseProxy's internal errors (websocket upgrade
	// failures, hijack errors, response-write errors after a partial
	// flush) through structured slog instead of the package-level
	// log.Default(), which writes plain text to stderr and bypasses our
	// audit/log pipeline. Warn level matches the operational severity:
	// these are real failures the operator wants to see, but they do not
	// indicate a security boundary breach.
	rp.ErrorLog = slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn)

	// Resolve config / mainOrigin once for the lifetime of this request.
	cfg := configFromContext(r.Context())
	if cfg == nil {
		cfg = a.agentLoop.GetConfig()
	}
	mainOrigin := resolveMainOrigin(cfg)

	// Track whether we have decided to emit dev.proxied for this token's
	// first served response. We compute this BEFORE the proxy writes
	// anything so the decision is stable across header rewrites.
	emitFirstServed := a.markFirstServed(token)

	// Rewrite the request URL so the dev app sees its own paths. The
	// default Director would forward "/dev/<agent>/<token>/foo" verbatim;
	// dev apps don't know about that prefix and would 404. Save the
	// existing director to compose with our rewrite.
	origDirector := rp.Director
	rp.Director = func(req *http.Request) {
		origDirector(req)
		req.URL.Path = "/" + remaining
		// RawPath defaults to empty (default re-encoding kicks in);
		// clear it to avoid stale double-encoded values from the
		// inbound request.
		req.URL.RawPath = ""
		// Strip any inbound Authorization header so a forwarded
		// user-supplied bearer (or stale gateway token) cannot reach the
		// dev server. The /dev/ route uses path-token-only auth (FR-023);
		// Authorization is irrelevant here.
		req.Header.Del("Authorization")
		// X-Forwarded-* headers help dev apps log meaningful client
		// info even though the connection comes from loopback.
		req.Header.Set("X-Forwarded-Host", r.Host)
		req.Header.Set("X-Forwarded-Proto", schemeFromRequest(r))
	}

	// FR-007d: strip upstream CSP/XFO so the gateway-injected policy is
	// authoritative. Without this the browser intersects the dev
	// server's CSP (which permits 'unsafe-eval' for HMR) with ours,
	// producing a confusing combined policy.
	//
	// FR-007 / FR-007b: inject the gateway's CSP, Referrer-Policy and
	// X-Content-Type-Options on every proxied response so the dev iframe
	// is treated identically to /serve/ from a security-header
	// perspective.
	rp.ModifyResponse = func(resp *http.Response) error {
		// Strip upstream policy headers BEFORE we inject ours.
		resp.Header.Del("Content-Security-Policy")
		resp.Header.Del("Content-Security-Policy-Report-Only")
		resp.Header.Del("X-Frame-Options")
		// Inject gateway-authoritative headers. Use the same helper as
		// /serve/ so any future change applies symmetrically.
		setWorkspaceSecurityHeaders(responseHeaderWriter{resp.Header}, mainOrigin)
		// FR-024: emit dev.proxied on the FIRST proxied response per
		// token only. We use Info level — the spec calls this Debug-
		// default but Omnipus's audit primitive does not currently
		// distinguish levels at the entry layer; the slog mirror in
		// emitPreviewAuditEntry uses level. Status comes from the
		// upstream response — captures both 200s and dev-server 5xx
		// (the operator wants both signals).
		//
		// F-11: bytes_out is intentionally omitted (pass -1 so
		// emitPreviewAuditEntry drops the field). For chunked responses
		// resp.ContentLength == -1 and recording it silently produced no
		// bytes_out field anyway. The dev server's own telemetry tracks
		// bandwidth more accurately; FR-024 marks bytes_out as optional.
		if emitFirstServed {
			a.auditDevSuccess(r, "dev.proxied", agentID, token, resp.StatusCode, startedAt, -1)
		}
		return nil
	}

	// Custom error handler so a crashed dev server returns 502 with a
	// stderr-friendly message rather than the default text/plain
	// "Bad Gateway" body. Also emits a dev.upstream_unreachable failure
	// audit so operators see crash-loop dev servers.
	//
	// F-30: dedup audit emission per-(token, remoteIP) within a 60s
	// window. A flapping dev server can fire dozens of failures per
	// second under HMR reconnects; without dedup every retry pollutes
	// the audit log with effectively-identical entries. The helper
	// returns a "suppressed_count" on the next admit so operators still
	// see the magnitude of the swallowed period — no information loss,
	// just compressed into a single Warn line.
	rp.ErrorHandler = func(rw http.ResponseWriter, errReq *http.Request, err error) {
		// Compute remote IP from the inbound request. net.SplitHostPort
		// returns an error for already-stripped strings (rare in practice
		// but possible with non-standard transports); fall back to the
		// raw RemoteAddr in that case so the dedup key is stable.
		remoteIP := r.RemoteAddr
		if host, _, splitErr := net.SplitHostPort(r.RemoteAddr); splitErr == nil {
			remoteIP = host
		}
		admit, suppressedCount := markFirstUpstreamFailure(token, remoteIP)
		if admit {
			if suppressedCount > 0 {
				// Surface the swallowed-failure count from the previous
				// suppression window. Operators tailing logs see a single
				// breadcrumb summarising the flap rather than per-failure
				// noise. Keyed by token + remoteIP so two clients hitting
				// the same flapping server each get their own counter.
				// Token prefix uses the same sanitisation as the audit
				// schema (first 8 chars, validated against the base64-RawURL
				// alphabet) so the log line never carries attacker-controlled
				// bytes.
				prefix := token
				if len(prefix) > 8 {
					prefix = prefix[:8]
				}
				if !tokenPrefixRE.MatchString(prefix) {
					prefix = "<invalid>"
				}
				slog.Warn("dev.upstream_unreachable: window reset; previous failures were suppressed",
					"token_prefix", prefix,
					"remote_ip", remoteIP,
					"suppressed_count", suppressedCount,
				)
			}
			a.auditDevFailure(r, "dev.upstream_unreachable", "error", agentID, token, http.StatusBadGateway, startedAt)
		}
		writeDevProxyError(rw, http.StatusBadGateway,
			fmt.Sprintf("dev server unreachable on port %d: %v", reg.Port, err))
		_ = errReq
	}

	rp.ServeHTTP(w, r)
}

// schemeFromRequest derives "https" or "http" for the X-Forwarded-Proto
// header. We trust X-Forwarded-Proto if present (gateway is typically
// behind a reverse proxy in production); otherwise we look at r.TLS.
func schemeFromRequest(r *http.Request) string {
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return proto
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// writeDevProxyError writes a JSON error matching the gateway's canonical
// shape. Local helper — pkg/gateway already has jsonErr but it sets
// Content-Type after WriteHeader which writes the header default; using
// our own ensures the JSON header lands first.
func writeDevProxyError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	// Manual JSON encoding to avoid pulling encoding/json just for one
	// shape. msg is already trusted (constants or stringified errors).
	_, _ = fmt.Fprintf(w, `{"error":%q}`, msg)
}

// auditDevSuccess emits a dev.proxied event at Info (FR-024). Only the
// first request per token reaches here (gated by markFirstServed in
// proxyDevRequest).
func (a *restAPI) auditDevSuccess(
	r *http.Request,
	event string,
	agentID, token string,
	status int,
	startedAt time.Time,
	bytesOut int64,
) {
	a.emitPreviewAuditEntry(r, event, "allow", agentID, token, status, startedAt, bytesOut, slog.LevelInfo)
}

// auditDevFailure emits a dev.* failure event at Warn (FR-024a).
// Decision: "deny" for 401/403, "error" for 400/404/502.
func (a *restAPI) auditDevFailure(
	r *http.Request,
	event string,
	decision string,
	agentID, token string,
	status int,
	startedAt time.Time,
) {
	a.emitPreviewAuditEntry(r, event, decision, agentID, token, status, startedAt, 0, slog.LevelWarn)
}

// responseHeaderWriter adapts http.Header to the http.ResponseWriter
// interface for the `setWorkspaceSecurityHeaders` helper, which only
// needs Header() to set string values. The other methods are no-ops —
// the helper does NOT call them.
//
// Use case: in proxyDevRequest the ModifyResponse callback receives an
// http.Response whose only header surface is .Header. The
// setWorkspaceSecurityHeaders helper expects an http.ResponseWriter; we
// adapt rather than duplicating the header-setting logic.
type responseHeaderWriter struct {
	h http.Header
}

func (rhw responseHeaderWriter) Header() http.Header { return rhw.h }
func (rhw responseHeaderWriter) Write([]byte) (int, error) {
	// Never invoked — setWorkspaceSecurityHeaders only calls Header().
	return 0, nil
}
func (rhw responseHeaderWriter) WriteHeader(int) {
	// Never invoked — setWorkspaceSecurityHeaders only calls Header().
}
