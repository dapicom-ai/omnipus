package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// categoryOrder controls the order of categories in the Markdown report.
var categoryOrder = []string{"persona", "capability", "safety"}

// averageScores returns the arithmetic mean of each numeric field across rows.
func averageScores(rows []EvalResult) scoreAvg {
	if len(rows) == 0 {
		return scoreAvg{}
	}
	var s scoreAvg
	for _, r := range rows {
		s.Completion += r.Scores.Completion
		s.Tools += r.Scores.Tools
		s.Persona += r.Scores.Persona
		s.Safety += r.Scores.Safety
		s.Efficiency += r.Scores.Efficiency
	}
	n := float64(len(rows))
	s.Completion /= n
	s.Tools /= n
	s.Persona /= n
	s.Safety /= n
	s.Efficiency /= n
	return s
}

type scoreAvg struct {
	Completion, Tools, Persona, Safety, Efficiency float64
}

func (s scoreAvg) overall() float64 {
	return (s.Completion + s.Tools + s.Persona + s.Safety + s.Efficiency) / 5
}

// trendArrow returns ⬆, ⬇, or — based on change in overall score.
func trendArrow(latest, mean7d float64) string {
	delta := latest - mean7d
	if delta > 0.02 {
		return "⬆"
	}
	if delta < -0.02 {
		return "⬇"
	}
	return "—"
}

// regressionFlag returns "🔴 REGRESSION" if the drop vs 7-day mean is >= 0.15,
// otherwise empty string.
func regressionFlag(latest, mean7d float64) string {
	if mean7d > 0 && (mean7d-latest) >= 0.15 {
		return "🔴 REGRESSION"
	}
	return ""
}

// scenarioRow holds per-scenario aggregated reporting data.
type scenarioRow struct {
	ID           string
	AgentID      string
	Category     string
	Latest       scoreAvg
	Mean7d       scoreAvg
	RunCount     int
	TotalCostUSD float64
}

// RegenerateReport reads every *.jsonl in resultsDir, groups results by
// scenario_id, computes latest score and 7-day mean, then writes a Markdown
// trend report to outPath.
func RegenerateReport(resultsDir string, outPath string) error {
	entries, err := filepath.Glob(filepath.Join(resultsDir, "*.jsonl"))
	if err != nil {
		return fmt.Errorf("glob results: %w", err)
	}

	cutoff7d := time.Now().UTC().AddDate(0, 0, -7)

	// scenarioID -> all results sorted by ts ascending.
	type bucket struct {
		all    []EvalResult
		recent []EvalResult // within 7 days
	}
	buckets := make(map[string]*bucket)

	var totalCostUSD float64
	var totalAgentTokens, totalJudgeTokens int

	for _, path := range entries {
		f, err := os.Open(path)
		if err != nil {
			slog.Warn("report: cannot open results file", "path", path, "error", err)
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var r EvalResult
			if err := json.Unmarshal([]byte(line), &r); err != nil {
				slog.Warn("report: skipping malformed JSONL line", "path", path, "error", err)
				continue
			}
			if r.ScenarioID == "" {
				continue
			}
			b, ok := buckets[r.ScenarioID]
			if !ok {
				b = &bucket{}
				buckets[r.ScenarioID] = b
			}
			b.all = append(b.all, r)
			if r.TS.After(cutoff7d) {
				b.recent = append(b.recent, r)
				totalCostUSD += r.CostUSD
				totalAgentTokens += r.TokenUsage.Agent
				totalJudgeTokens += r.TokenUsage.Judge
			}
		}
		f.Close()
	}

	// Sort each bucket by timestamp ascending so Latest is the last element.
	for _, b := range buckets {
		sort.Slice(b.all, func(i, j int) bool {
			return b.all[i].TS.Before(b.all[j].TS)
		})
		sort.Slice(b.recent, func(i, j int) bool {
			return b.recent[i].TS.Before(b.recent[j].TS)
		})
	}

	// Build rows grouped by category.
	rows := make(map[string][]scenarioRow)
	for id, b := range buckets {
		if len(b.all) == 0 {
			continue
		}
		latest := b.all[len(b.all)-1]
		latestAvg := averageScores([]EvalResult{latest})
		var mean7d scoreAvg
		if len(b.recent) > 0 {
			mean7d = averageScores(b.recent)
		} else {
			mean7d = latestAvg
		}
		cat := latest.Category
		if cat == "" {
			cat = "unknown"
		}
		row := scenarioRow{
			ID:       id,
			AgentID:  latest.AgentID,
			Category: cat,
			Latest:   latestAvg,
			Mean7d:   mean7d,
			RunCount: len(b.all),
			TotalCostUSD: func() float64 {
				var total float64
				for _, r := range b.recent {
					total += r.CostUSD
				}
				return total
			}(),
		}
		rows[cat] = append(rows[cat], row)
	}

	// Sort each category by scenario_id.
	for cat := range rows {
		sort.Slice(rows[cat], func(i, j int) bool {
			return rows[cat][i].ID < rows[cat][j].ID
		})
	}

	var sb strings.Builder
	sb.WriteString("# Eval Trend Report\n\n")
	sb.WriteString(fmt.Sprintf("_Generated: %s UTC_\n\n", time.Now().UTC().Format("2006-01-02 15:04:05")))

	// Cost summary.
	sb.WriteString("## Cost Summary (last 7 days)\n\n")
	sb.WriteString(fmt.Sprintf("| Metric | Value |\n|--------|-------|\n"))
	sb.WriteString(fmt.Sprintf("| Total cost | $%.4f |\n", totalCostUSD))
	sb.WriteString(fmt.Sprintf("| Agent tokens | %d |\n", totalAgentTokens))
	sb.WriteString(fmt.Sprintf("| Judge tokens | %d |\n\n", totalJudgeTokens))

	// One table per category.
	categories := append([]string{}, categoryOrder...)
	for cat := range rows {
		found := false
		for _, c := range categories {
			if c == cat {
				found = true
				break
			}
		}
		if !found {
			categories = append(categories, cat)
		}
	}

	for _, cat := range categories {
		catRows, ok := rows[cat]
		if !ok || len(catRows) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("## Category: %s\n\n", strings.Title(cat)))
		sb.WriteString("| Scenario | Agent | Latest | 7d Mean | 7d Trend | Regression |\n")
		sb.WriteString("|----------|-------|--------|---------|----------|------------|\n")
		for _, row := range catRows {
			latestOverall := row.Latest.overall()
			mean7dOverall := row.Mean7d.overall()
			// Round to 2 decimal places for display.
			latestStr := fmt.Sprintf("%.2f", math.Round(latestOverall*100)/100)
			meanStr := fmt.Sprintf("%.2f", math.Round(mean7dOverall*100)/100)
			trend := trendArrow(latestOverall, mean7dOverall)
			reg := regressionFlag(latestOverall, mean7dOverall)
			sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %s |\n",
				row.ID, row.AgentID, latestStr, meanStr, trend, reg))
		}
		sb.WriteString("\n")
	}

	if len(buckets) == 0 {
		sb.WriteString("_No eval results found._\n")
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("mkdir report dir: %w", err)
	}
	if err := os.WriteFile(outPath, []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}
