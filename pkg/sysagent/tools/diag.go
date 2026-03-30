// Omnipus — System Agent Tools
// License: MIT
// Copyright (c) 2026 Omnipus contributors

package systools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dapicom-ai/omnipus/pkg/security"
	"github.com/dapicom-ai/omnipus/pkg/tools"
)

// ---- system.doctor.run ----

type DoctorRunTool struct{ deps *Deps }

func NewDoctorRunTool(d *Deps) *DoctorRunTool { return &DoctorRunTool{deps: d} }
func (t *DoctorRunTool) Name() string          { return "system.doctor.run" }
func (t *DoctorRunTool) Description() string {
	return "Run security diagnostics and return a risk score (0-100) with actionable recommendations. No parameters required."
}
func (t *DoctorRunTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *DoctorRunTool) Execute(_ context.Context, _ map[string]any) *tools.ToolResult {
	type issue struct {
		Severity       string `json:"severity"`
		Message        string `json:"message"`
		Recommendation string `json:"recommendation"`
	}

	var issues []issue
	checksPassed := 0
	checksFailed := 0

	// Check exec egress via the existing security.CheckExecEgress function.
	execCfg := security.DiagnosticConfig{
		ExecToolEnabled:     t.deps.Cfg.Tools.IsToolEnabled("exec"),
		ExecProxyEnabled:    false, // TODO(exec-proxy): Wire when exec proxy is implemented. Currently returns stub data.
		ExecAllowedBinaries: t.deps.Cfg.Tools.Exec.AllowedBinaries,
	}
	for _, w := range security.CheckExecEgress(execCfg) {
		issues = append(issues, issue{
			Severity:       "high",
			Message:        w.Message,
			Recommendation: fmt.Sprintf("Address %s by reviewing your security settings", w.Code),
		})
		checksFailed++
	}

	// Check credentials file permissions.
	credPath := filepath.Join(t.deps.Home, "credentials.json")
	if info, err := os.Stat(credPath); err == nil {
		if info.Mode()&0o077 != 0 {
			issues = append(issues, issue{
				Severity:       "high",
				Message:        "credentials.json is world/group-readable (permissions too open)",
				Recommendation: "Run: chmod 600 ~/.omnipus/credentials.json",
			})
			checksFailed++
		} else {
			checksPassed++
		}
	}

	// Check config file permissions.
	if info, err := os.Stat(t.deps.ConfigPath); err == nil {
		if info.Mode()&0o077 != 0 {
			issues = append(issues, issue{
				Severity:       "medium",
				Message:        "config.json is world/group-readable",
				Recommendation: "Run: chmod 600 ~/.omnipus/config.json",
			})
			checksFailed++
		} else {
			checksPassed++
		}
	}

	// Check audit log directory.
	auditDir := filepath.Join(t.deps.Home, "system")
	if _, err := os.Stat(auditDir); os.IsNotExist(err) {
		issues = append(issues, issue{
			Severity:       "medium",
			Message:        "Audit log directory ~/.omnipus/system/ does not exist",
			Recommendation: "Restart Omnipus to recreate the directory structure",
		})
		checksFailed++
	} else {
		checksPassed++
	}

	total := checksPassed + checksFailed
	if total == 0 {
		total = 1
	}
	riskScore := checksFailed * 100 / total

	return tools.NewToolResult(successJSON(map[string]any{
		"risk_score":     riskScore,
		"issues":         issues,
		"checks_passed":  checksPassed,
		"checks_failed":  checksFailed,
		"run_at":         time.Now().UTC().Format(time.RFC3339),
	}))
}

// ---- system.backup.create ----

type BackupCreateTool struct{ deps *Deps }

func NewBackupCreateTool(d *Deps) *BackupCreateTool { return &BackupCreateTool{deps: d} }
func (t *BackupCreateTool) Name() string             { return "system.backup.create" }
func (t *BackupCreateTool) Description() string {
	return "Create a backup of the Omnipus data directory. Parameters: encrypt (bool, default false)."
}
func (t *BackupCreateTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"encrypt": map[string]any{"type": "boolean"}},
	}
}
func (t *BackupCreateTool) Execute(_ context.Context, args map[string]any) *tools.ToolResult {
	encrypt, _ := args["encrypt"].(bool)
	backupsDir := filepath.Join(t.deps.Home, "backups")
	if err := os.MkdirAll(backupsDir, 0o700); err != nil {
		return tools.ErrorResult(errorJSON("BACKUP_FAILED", err.Error(), ""))
	}
	timestamp := time.Now().UTC().Format("2006-01-02T15-04-05Z")
	suffix := ".tar"
	if encrypt {
		suffix = ".tar.enc"
	}
	backupPath := filepath.Join(backupsDir, fmt.Sprintf("omnipus-backup-%s%s", timestamp, suffix))
	// Stub: returns the planned backup path but does not create the archive yet.
	return tools.NewToolResult(successJSON(map[string]any{
		"path":       backupPath,
		"size_bytes": 0,
		"encrypted":  encrypt,
		"created_at": timestamp,
		"note":       "Backup file creation is not yet implemented — path reserved",
	}))
}

// ---- system.cost.query ----

type CostQueryTool struct{ deps *Deps }

func NewCostQueryTool(d *Deps) *CostQueryTool { return &CostQueryTool{deps: d} }
func (t *CostQueryTool) Name() string          { return "system.cost.query" }
func (t *CostQueryTool) Description() string {
	return "Query LLM cost data by period. Parameters: period (today/week/month/custom), start_date, end_date, agent_id, group_by."
}
func (t *CostQueryTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"period":     map[string]any{"type": "string"},
			"start_date": map[string]any{"type": "string"},
			"end_date":   map[string]any{"type": "string"},
			"agent_id":   map[string]any{"type": "string"},
			"group_by":   map[string]any{"type": "string"},
		},
	}
}
func (t *CostQueryTool) Execute(_ context.Context, args map[string]any) *tools.ToolResult {
	period, _ := args["period"].(string)
	if period == "" {
		period = "today"
	}
	return tools.NewToolResult(successJSON(map[string]any{
		"period":       period,
		"total_cost":   0.0,
		"total_tokens": 0,
		"breakdown":    []any{},
		"note":         "Cost tracking from session transcripts is not yet aggregated in this version",
	}))
}
