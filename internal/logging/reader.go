package logging

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultLogLimit = 5
	maxLogLineBytes = 16 * 1024 * 1024
)

type LogQuery struct {
	Prefix        string
	Limit         int
	Days          int
	MinLevel      string
	Since         *time.Time
	Until         *time.Time
	Fields        map[string]string
	FieldExists   []string
	FieldContains map[string][]string
	Contains      string
	MsgContains   string
	Raw           bool
}

type LogEntry struct {
	Time    time.Time
	Level   string
	Message string
	Fields  map[string]string
	Raw     string
}

// QueryResult 是一次日志扫描的结果。损坏行（解析不出 time/level/msg 的行）会被跳过
// 并计数：它们既不该占用 `--limit` 配额，也不该在结果里冒充一条记录。
type QueryResult struct {
	Entries []LogEntry
	// DamagedCount 是本次扫描遇到的损坏行数，不是整个文件的行数。
	DamagedCount int
	// DamagedLines 是损坏行的原始内容样本（最多 damagedSampleLimit 条），只在
	// `--raw` 查询里收集，且不参与任何筛选匹配。
	DamagedLines []string
}

type Reader struct {
	Dir string
}

// damagedSampleLimit 限制损坏行样本的条数：提示用，不追求完整。
const damagedSampleLimit = 10

func (r Reader) Query(ctx context.Context, query LogQuery) (QueryResult, error) {
	if strings.TrimSpace(r.Dir) == "" {
		return QueryResult{}, fmt.Errorf("log directory is not configured")
	}
	query = normalizeLogQuery(query)
	result := QueryResult{Entries: []LogEntry{}}
	for _, path := range logPaths(r.Dir, query.Prefix, query.Days) {
		if err := ctx.Err(); err != nil {
			return QueryResult{}, err
		}
		fileResult, err := readLogFile(path, query.Raw)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return QueryResult{}, err
		}
		result.DamagedCount += fileResult.DamagedCount
		result.DamagedLines = append(result.DamagedLines, fileResult.DamagedLines...)
		if len(result.DamagedLines) > damagedSampleLimit {
			result.DamagedLines = result.DamagedLines[:damagedSampleLimit]
		}
		for i := len(fileResult.Entries) - 1; i >= 0; i-- {
			entry := fileResult.Entries[i]
			if matchLogEntry(entry, query) {
				result.Entries = append(result.Entries, entry)
				if len(result.Entries) >= query.Limit {
					return result, nil
				}
			}
		}
	}
	return result, nil
}

func normalizeLogQuery(query LogQuery) LogQuery {
	query.Prefix = strings.TrimSpace(query.Prefix)
	if query.Limit <= 0 {
		query.Limit = DefaultLogLimit
	}
	if query.Days <= 0 {
		query.Days = 1
	}
	query.MinLevel = strings.ToUpper(strings.TrimSpace(query.MinLevel))
	if query.Fields == nil {
		query.Fields = map[string]string{}
	}
	if query.FieldContains == nil {
		query.FieldContains = map[string][]string{}
	}
	return query
}

func logPaths(dir, prefix string, days int) []string {
	paths := make([]string, 0, days)
	today := time.Now()
	for i := 0; i < days; i++ {
		day := today.AddDate(0, 0, -i).Format("2006-01-02")
		paths = append(paths, filepath.Join(dir, prefix+"-"+day+".log"))
	}
	return paths
}

func readLogFile(path string, collectDamaged bool) (QueryResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return QueryResult{}, err
	}
	defer file.Close()

	result := QueryResult{Entries: []LogEntry{}}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxLogLineBytes)
	for scanner.Scan() {
		raw := scanner.Text()
		if strings.TrimSpace(raw) == "" {
			continue
		}
		entry, damaged := parseLogLine(raw)
		if damaged {
			result.DamagedCount++
			if collectDamaged && len(result.DamagedLines) < damagedSampleLimit {
				result.DamagedLines = append(result.DamagedLines, raw)
			}
			continue
		}
		result.Entries = append(result.Entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return QueryResult{}, fmt.Errorf("read log file %s: %w", path, err)
	}
	return result, nil
}

// parseLogLine 解析一行日志。损坏行（解析不出 time/level/msg 任意一个字段的行，例如
// 被截断的半行、早期格式或非本系统的内容）由第二个返回值报告，调用方负责跳过与计数。
func parseLogLine(raw string) (LogEntry, bool) {
	fields := parseTextAttrs(raw)
	entry := LogEntry{Raw: raw, Fields: fields, Level: fields["level"], Message: fields["msg"]}
	if ts, err := time.ParseInLocation("2006-01-02 15:04:05", fields["time"], time.Local); err == nil {
		entry.Time = ts
	}
	if entry.Level == "" && entry.Message == "" && strings.TrimSpace(fields["time"]) == "" {
		return LogEntry{Raw: raw, Fields: map[string]string{}}, true
	}
	return entry, false
}

func parseTextAttrs(line string) map[string]string {
	out := map[string]string{}
	for i := 0; i < len(line); {
		for i < len(line) && line[i] == ' ' {
			i++
		}
		start := i
		for i < len(line) && line[i] != '=' && line[i] != ' ' {
			i++
		}
		if i >= len(line) || line[i] != '=' || start == i {
			for i < len(line) && line[i] != ' ' {
				i++
			}
			continue
		}
		key := line[start:i]
		i++
		value, next := parseTextValue(line, i)
		out[key] = value
		i = next
	}
	return out
}

func parseTextValue(line string, start int) (string, int) {
	if start >= len(line) {
		return "", start
	}
	if line[start] != '"' {
		i := start
		for i < len(line) && line[i] != ' ' {
			i++
		}
		return line[start:i], i
	}
	i := start + 1
	escaped := false
	for i < len(line) {
		c := line[i]
		if c == '\\' && !escaped {
			escaped = true
			i++
			continue
		}
		if c == '"' && !escaped {
			quoted := line[start : i+1]
			if unquoted, err := strconv.Unquote(quoted); err == nil {
				return unquoted, i + 1
			}
			return line[start+1 : i], i + 1
		}
		escaped = false
		i++
	}
	return line[start+1:], len(line)
}

func matchLogEntry(entry LogEntry, query LogQuery) bool {
	if query.Since != nil && !entry.Time.IsZero() && entry.Time.Before(*query.Since) {
		return false
	}
	if query.Until != nil && !entry.Time.IsZero() && entry.Time.After(*query.Until) {
		return false
	}
	if query.MinLevel != "" && !levelAtLeast(entry.Level, query.MinLevel) {
		return false
	}
	for key, want := range query.Fields {
		if strings.TrimSpace(want) == "" {
			continue
		}
		if !strings.EqualFold(entry.Fields[key], want) {
			return false
		}
	}
	for _, key := range query.FieldExists {
		if strings.TrimSpace(entry.Fields[key]) == "" {
			return false
		}
	}
	for key, needles := range query.FieldContains {
		fieldValue := entry.Fields[key]
		for _, needle := range needles {
			if strings.TrimSpace(needle) != "" && !containsFold(fieldValue, needle) {
				return false
			}
		}
	}
	if query.Contains != "" && !logEntryContains(entry, query.Contains) {
		return false
	}
	if query.MsgContains != "" && !containsFold(entry.Message, query.MsgContains) {
		return false
	}
	return true
}

func logEntryContains(entry LogEntry, needle string) bool {
	for _, key := range []string{"text", "raw_text", "arguments", "result", "latest_message_json", "first_system_message_json"} {
		if containsFold(entry.Fields[key], needle) {
			return true
		}
	}
	return containsFold(entry.Raw, needle)
}

func containsFold(value, needle string) bool {
	return strings.Contains(strings.ToLower(value), strings.ToLower(needle))
}

func levelAtLeast(got, min string) bool {
	return levelRank(got) >= levelRank(min)
}

func levelRank(level string) int {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "DEBUG":
		return 0
	case "INFO":
		return 1
	case "WARN", "WARNING":
		return 2
	case "ERROR":
		return 3
	default:
		return 1
	}
}
