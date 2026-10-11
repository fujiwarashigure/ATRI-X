package commands

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/fujiwarashigure/ATRI-X/internal/command"
	"github.com/fujiwarashigure/ATRI-X/internal/logging"
	"github.com/fujiwarashigure/ATRI-X/internal/security"
)

const usageQueryLimit = 100000

func usageInfo() command.Info {
	return command.Info{
		Name:        "usage",
		Usage:       "/usage [options]",
		Description: "Aggregate token usage from audit logs.",
		MinRole:     security.RoleSuperadmin,
		Help: strings.TrimSpace(`Options:
  -d, --days <n>        Days to look back. Default: 1.
  -m, --model <name>    Filter by model name.
  -s, --session <id>    Filter by session ID.
  --by <key>            Group by: model (default), day, session.
  --since <time>        Show usage after a time, e.g. 2h, 30m, 2026-06-03.
  --until <time>        Show usage before a time.

Examples:
  /usage
  /usage -d 7
  /usage -m gpt-4o
  /usage -s sess-xxx
  /usage --by day -d 30
  /usage --since 2h`),
	}
}

func NewUsage(deps Deps) command.Handler {
	return usageCommand{deps: deps}
}

type usageCommand struct {
	deps Deps
}

func (c usageCommand) Info() command.Info { return usageInfo() }

func (c usageCommand) Handle(ctx context.Context, req command.Request) (*command.Result, error) {
	if wantsCommandHelp(req.Args) {
		return formatCommandHelp(req.Prefix, usageInfo()), nil
	}
	query, groupBy, err := parseUsageQuery(req.Args)
	if err != nil {
		return nil, err
	}
	if c.deps.Logs == nil {
		return &command.Result{Content: "log reader is not configured"}, nil
	}
	scanned, err := c.deps.Logs.QueryLogs(ctx, query)
	if err != nil {
		return nil, err
	}
	if len(scanned.Entries) == 0 {
		return &command.Result{Content: strings.TrimSpace("no usage data found\n" + usageDamagedNotice(scanned))}, nil
	}
	return &command.Result{Content: formatUsageSummary(scanned.Entries, query, groupBy) + usageDamagedNotice(scanned)}, nil
}

// usageDamagedNotice 提醒这次统计跳过了损坏行：用量统计本来就可能因为"没有记录用量"
// 而偏高或偏低，但整行读不出来是另一种不完整，必须与 null 区分开。
func usageDamagedNotice(scanned logging.QueryResult) string {
	if scanned.DamagedCount <= 0 {
		return ""
	}
	return fmt.Sprintf("\n\n本次扫描跳过 %d 条损坏记录（无法解析 time/level/msg），用量统计可能不完整。", scanned.DamagedCount)
}

func (c usageCommand) Complete(ctx context.Context, req command.CompletionRequest) []command.Completion {
	_ = ctx
	token := currentCompletionToken(req)
	previous := previousLogToken(req.Raw, token.Start)
	if previous == "--by" {
		return completeStringOptions([]string{"model", "day", "session"}, token.Text, token.Start, token.End, "group_by")
	}
	if strings.HasPrefix(token.Text, "-") || token.Text == "" {
		return completeStaticOptions(usageCompletionOptions(), token.Text, token.Start, token.End, "option")
	}
	return nil
}

func usageCompletionOptions() []completionOption {
	return []completionOption{
		{Text: "-d", Description: "Days to look back"},
		{Text: "--days", Description: "Days to look back"},
		{Text: "-m", Description: "Filter by model"},
		{Text: "--model", Description: "Filter by model"},
		{Text: "-s", Description: "Filter by session"},
		{Text: "--session", Description: "Filter by session"},
		{Text: "--by", Description: "Group by model/day/session"},
		{Text: "--since", Description: "Show after time"},
		{Text: "--until", Description: "Show before time"},
	}
}

type usageGroupKey string

const (
	groupByModel   usageGroupKey = "model"
	groupByDay     usageGroupKey = "day"
	groupBySession usageGroupKey = "session"
)

func parseUsageQuery(args string) (logging.LogQuery, usageGroupKey, error) {
	query := logging.LogQuery{
		Prefix:        "audit",
		Limit:         usageQueryLimit,
		Days:          1,
		MinLevel:      "debug",
		Fields:        map[string]string{"event": "llm_usage"},
		FieldContains: map[string][]string{},
	}
	groupBy := groupByModel

	parts, err := splitLogArgs(args)
	if err != nil {
		return query, groupBy, err
	}
	for i := 0; i < len(parts); i++ {
		name := parts[i]
		switch name {
		case "-d", "--days":
			value, err := nextArg(parts, &i, name)
			if err != nil {
				return query, groupBy, err
			}
			days, err := parsePositiveInt(value, "days")
			if err != nil {
				return query, groupBy, err
			}
			query.Days = days
		case "-m", "--model":
			value, err := nextArg(parts, &i, name)
			if err != nil {
				return query, groupBy, err
			}
			query.Fields["model"] = value
		case "-s", "--session":
			value, err := nextArg(parts, &i, name)
			if err != nil {
				return query, groupBy, err
			}
			query.Fields["session_id"] = value
		case "--by":
			value, err := nextArg(parts, &i, name)
			if err != nil {
				return query, groupBy, err
			}
			switch strings.ToLower(strings.TrimSpace(value)) {
			case "model", "day", "session":
				groupBy = usageGroupKey(strings.ToLower(strings.TrimSpace(value)))
			default:
				return query, groupBy, fmt.Errorf("--by must be model, day or session")
			}
		case "--since":
			value, err := nextArg(parts, &i, name)
			if err != nil {
				return query, groupBy, err
			}
			parsed, err := parseLogTime(value)
			if err != nil {
				return query, groupBy, err
			}
			query.Since = &parsed
		case "--until":
			value, err := nextArg(parts, &i, name)
			if err != nil {
				return query, groupBy, err
			}
			parsed, err := parseLogTime(value)
			if err != nil {
				return query, groupBy, err
			}
			query.Until = &parsed
		default:
			return query, groupBy, fmt.Errorf("unknown option: %s", name)
		}
	}
	return query, groupBy, nil
}

// usageValue 是"已知数值 / 未知"的三态值。未知（字段缺失、非法、溢出）在输出里显示 null，
// 并向上传递到受影响的分组与总计项——否则"没记录"会和"真的是 0"混成同一样东西，
// 让统计看起来完整而实际不是。
type usageValue struct {
	known bool
	value int64
}

// knownUsageValue 构造一个已知数值；累计器必须用它起步，零值 usageValue 表示未知。
func knownUsageValue(value int64) usageValue {
	return usageValue{known: true, value: value}
}

// add 并入一个来源值：任一来源未知，累计结果就未知。
func (v usageValue) add(other usageValue) usageValue {
	if !v.known || !other.known {
		return usageValue{}
	}
	return usageValue{known: true, value: v.value + other.value}
}

func (v usageValue) format() string {
	if !v.known {
		return "null"
	}
	return formatThousands(v.value)
}

// parseUsageValue 解析一个用量字段：字段缺失、非法值和溢出都算未知，显式的 0 是已知的 0。
func parseUsageValue(raw string) usageValue {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return usageValue{}
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return usageValue{}
	}
	return knownUsageValue(n)
}

type usageBucket struct {
	key              string
	calls            int
	promptTokens     usageValue
	completionTokens usageValue
	totalTokens      usageValue
	cacheHitTokens   usageValue
	elapsedMs        usageValue
}

func newUsageBucket(key string) *usageBucket {
	return &usageBucket{
		key:              key,
		promptTokens:     knownUsageValue(0),
		completionTokens: knownUsageValue(0),
		totalTokens:      knownUsageValue(0),
		cacheHitTokens:   knownUsageValue(0),
		elapsedMs:        knownUsageValue(0),
	}
}

func (b *usageBucket) addEntry(entry logging.LogEntry) {
	b.calls++
	b.promptTokens = b.promptTokens.add(parseUsageValue(entry.Fields["prompt_tokens"]))
	b.completionTokens = b.completionTokens.add(parseUsageValue(entry.Fields["completion_tokens"]))
	b.totalTokens = b.totalTokens.add(parseUsageValue(entry.Fields["total_tokens"]))
	b.cacheHitTokens = b.cacheHitTokens.add(parseUsageValue(entry.Fields["cache_hit_tokens"]))
	b.elapsedMs = b.elapsedMs.add(parseUsageValue(entry.Fields["elapsed_ms"]))
}

func formatUsageSummary(entries []logging.LogEntry, query logging.LogQuery, groupBy usageGroupKey) string {
	buckets := map[string]*usageBucket{}
	var order []string
	grand := newUsageBucket("total")

	for _, entry := range entries {
		key := usageGroupKeyFor(entry, groupBy)
		b := buckets[key]
		if b == nil {
			b = newUsageBucket(key)
			buckets[key] = b
			order = append(order, key)
		}
		b.addEntry(entry)
		grand.addEntry(entry)
	}

	sort.Strings(order)

	rangeDesc := usageRangeDesc(query)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("token usage (%s, by %s):\n\n", rangeDesc, groupBy))
	for _, key := range order {
		b := buckets[key]
		formatUsageBucket(&sb, key, b)
		sb.WriteString("\n")
	}
	sb.WriteString("─────────────────────────────\n")
	formatUsageBucket(&sb, "total", grand)
	return sb.String()
}

func usageGroupKeyFor(entry logging.LogEntry, groupBy usageGroupKey) string {
	switch groupBy {
	case groupByDay:
		if entry.Time.IsZero() {
			return "unknown-date"
		}
		return entry.Time.Format("2006-01-02")
	case groupBySession:
		return firstNonEmpty(entry.Fields["session_id"], "unknown-session")
	default:
		provider := strings.TrimSpace(entry.Fields["provider"])
		model := strings.TrimSpace(entry.Fields["model"])
		if provider != "" && model != "" {
			return provider + "/" + model
		}
		return firstNonEmpty(model, provider, "unknown-model")
	}
}

func usageRangeDesc(query logging.LogQuery) string {
	parts := []string{}
	if query.Since != nil {
		parts = append(parts, "since "+query.Since.Format("2006-01-02 15:04"))
	}
	if query.Until != nil {
		parts = append(parts, "until "+query.Until.Format("2006-01-02 15:04"))
	}
	if len(parts) > 0 {
		return strings.Join(parts, ", ")
	}
	if query.Days > 0 {
		return fmt.Sprintf("last %d day(s)", query.Days)
	}
	return "all time"
}

func formatThousands(n int64) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	digits := strconv.FormatInt(n, 10)
	if len(digits) <= 3 {
		if negative {
			return "-" + digits
		}
		return digits
	}
	var sb strings.Builder
	rem := len(digits) % 3
	if rem > 0 {
		sb.WriteString(digits[:rem])
		if len(digits) > rem {
			sb.WriteByte(',')
		}
	}
	for i := rem; i < len(digits); i += 3 {
		sb.WriteString(digits[i : i+3])
		if i+3 < len(digits) {
			sb.WriteByte(',')
		}
	}
	if negative {
		return "-" + sb.String()
	}
	return sb.String()
}

func formatUsageBucket(sb *strings.Builder, label string, b *usageBucket) {
	sb.WriteString(fmt.Sprintf("[%s] calls: %d\n", label, b.calls))
	sb.WriteString(fmt.Sprintf("  prompt: %s | completion: %s | total: %s\n",
		b.promptTokens.format(),
		b.completionTokens.format(),
		b.totalTokens.format()))
	sb.WriteString(fmt.Sprintf("  cache: %s | elapsed: %s\n",
		b.cacheHitTokens.format(),
		formatElapsed(b.elapsedMs)))
}

func formatElapsed(elapsed usageValue) string {
	if !elapsed.known {
		return "null"
	}
	return fmt.Sprintf("%.1fs", float64(elapsed.value)/1000)
}
