package dashboard

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type claudeCodeUsage struct {
	Available     bool                  `json:"available"`
	Today         *claudeCodeDaySummary `json:"today,omitempty"`
	Last7Days     []claudeCodeDay       `json:"last_7_days,omitempty"`
	TotalSessions int                   `json:"total_sessions,omitempty"`
}

type claudeCodeDaySummary struct {
	Sessions         int     `json:"sessions"`
	InputTokens      int64   `json:"input_tokens"`
	OutputTokens     int64   `json:"output_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
}

type claudeCodeDay struct {
	Date         string  `json:"date"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

// jsonlMessage represents a single message in a Claude Code JSONL session file.
type jsonlMessage struct {
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message,omitempty"`
	// Usage fields can appear at the top level or nested
	Usage *jsonlUsage `json:"usage,omitempty"`
}

type jsonlUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

// Sonnet 4.6 pricing per million tokens (default assumption)
const (
	sonnetInputPricePerM  = 3.0
	sonnetOutputPricePerM = 15.0
	sonnetCachePricePerM  = 0.30
)

func estimateCost(inputTokens, outputTokens, cacheReadTokens int64) float64 {
	cost := float64(inputTokens) / 1_000_000 * sonnetInputPricePerM
	cost += float64(outputTokens) / 1_000_000 * sonnetOutputPricePerM
	cost += float64(cacheReadTokens) / 1_000_000 * sonnetCachePricePerM
	return cost
}

func parseClaudeCodeUsage() claudeCodeUsage {
	home, err := os.UserHomeDir()
	if err != nil {
		return claudeCodeUsage{Available: false}
	}

	projectsDir := filepath.Join(home, ".claude", "projects")
	if _, err := os.Stat(projectsDir); os.IsNotExist(err) {
		return claudeCodeUsage{Available: false}
	}

	// Collect all JSONL files modified in last 7 days
	cutoff := time.Now().AddDate(0, 0, -7)
	var sessionFiles []string

	_ = filepath.Walk(projectsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".jsonl") && info.ModTime().After(cutoff) {
			sessionFiles = append(sessionFiles, path)
		}
		return nil
	})

	if len(sessionFiles) == 0 {
		return claudeCodeUsage{Available: false}
	}

	// Aggregate by date
	dailyStats := make(map[string]*claudeCodeDaySummary)
	totalSessions := 0

	for _, path := range sessionFiles {
		totalSessions++
		info, _ := os.Stat(path)
		dateKey := info.ModTime().Format("2006-01-02")

		day, exists := dailyStats[dateKey]
		if !exists {
			day = &claudeCodeDaySummary{}
			dailyStats[dateKey] = day
		}
		day.Sessions++

		f, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // 10MB max line
		for scanner.Scan() {
			var msg jsonlMessage
			if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
				continue
			}
			if msg.Usage != nil {
				day.InputTokens += msg.Usage.InputTokens
				day.OutputTokens += msg.Usage.OutputTokens
				day.CacheReadTokens += msg.Usage.CacheReadInputTokens
			}
		}
		f.Close()
	}

	// Build response
	today := time.Now().Format("2006-01-02")
	result := claudeCodeUsage{
		Available:     true,
		TotalSessions: totalSessions,
	}

	if todayStats, ok := dailyStats[today]; ok {
		todayStats.EstimatedCostUSD = estimateCost(todayStats.InputTokens, todayStats.OutputTokens, todayStats.CacheReadTokens)
		result.Today = todayStats
	}

	// Sort days descending
	var days []claudeCodeDay
	for date, stats := range dailyStats {
		days = append(days, claudeCodeDay{
			Date:         date,
			InputTokens:  stats.InputTokens,
			OutputTokens: stats.OutputTokens,
			CostUSD:      estimateCost(stats.InputTokens, stats.OutputTokens, stats.CacheReadTokens),
		})
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Date > days[j].Date })
	result.Last7Days = days

	return result
}
