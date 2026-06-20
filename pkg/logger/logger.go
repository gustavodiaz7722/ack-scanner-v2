// Package logger provides structured, colorized logging for ack-scanner-v2.
// It outputs to stderr with timestamps, phase indicators, progress bars,
// and color-coded severity levels.
package logger

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Level represents log severity.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// ANSI color codes
const (
	reset   = "\033[0m"
	bold    = "\033[1m"
	dim     = "\033[2m"
	red     = "\033[31m"
	green   = "\033[32m"
	yellow  = "\033[33m"
	blue    = "\033[34m"
	magenta = "\033[35m"
	cyan    = "\033[36m"
	white   = "\033[37m"
)

// Phase icons
const (
	iconDiscover = "🔍"
	iconClone    = "📦"
	iconAgent    = "🤖"
	iconAnalyze  = "🔬"
	iconMatch    = "🔗"
	iconReport   = "📊"
	iconCache    = "💾"
	iconSkip     = "⏭️"
	iconDone     = "✅"
	iconFail     = "❌"
	iconWarn     = "⚠️"
	iconTime     = "⏱️"
	iconRetry    = "🔄"
)

// Logger provides structured logging with color and progress tracking.
type Logger struct {
	mu       sync.Mutex
	w        io.Writer
	level    Level
	color    bool
	start    time.Time
	phase    int
	maxPhase int
}

// New creates a new Logger. If color is true, ANSI codes are used.
func New(level Level, color bool) *Logger {
	return &Logger{
		w:        os.Stderr,
		level:    level,
		color:    color,
		start:    time.Now(),
		maxPhase: 6,
	}
}

// Nop returns a logger that discards all output.
func Nop() *Logger {
	return &Logger{
		w:     io.Discard,
		level: LevelError + 1,
	}
}

// SetPhase updates the current phase number.
func (l *Logger) SetPhase(phase int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.phase = phase
}

// SetMaxPhase updates the total number of phases displayed in the log prefix.
func (l *Logger) SetMaxPhase(max int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.maxPhase = max
}

// elapsed returns the formatted elapsed time since logger creation.
func (l *Logger) elapsed() string {
	d := time.Since(l.start)
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}

// write outputs a formatted log line.
func (l *Logger) write(level Level, icon, msg string) {
	if level < l.level {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	ts := l.elapsed()
	phase := ""
	if l.phase > 0 {
		phase = fmt.Sprintf("[%d/%d]", l.phase, l.maxPhase)
	}

	var line string
	if l.color {
		tsColored := dim + ts + reset
		phaseColored := cyan + phase + reset
		line = fmt.Sprintf("%s %s %s %s\n", tsColored, phaseColored, icon, msg)
	} else {
		line = fmt.Sprintf("%s %s %s %s\n", ts, phase, icon, msg)
	}

	fmt.Fprint(l.w, line)
}

// --- Public logging methods ---

// PhaseStart logs the beginning of a major phase with a header.
func (l *Logger) PhaseStart(phase int, title string) {
	l.SetPhase(phase)
	separator := strings.Repeat("─", 50)
	if l.color {
		l.mu.Lock()
		fmt.Fprintf(l.w, "\n%s%s%s\n", dim, separator, reset)
		l.mu.Unlock()
	} else {
		l.mu.Lock()
		fmt.Fprintf(l.w, "\n%s\n", separator)
		l.mu.Unlock()
	}
	l.write(LevelInfo, phaseIcon(phase), bold+title+reset)
}

// PhaseComplete logs the completion of a phase with a summary.
func (l *Logger) PhaseComplete(phase int, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if l.color {
		l.write(LevelInfo, iconDone, green+msg+reset)
	} else {
		l.write(LevelInfo, iconDone, msg)
	}
}

// Progress logs per-item progress within a phase.
func (l *Logger) Progress(current, total int, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	bar := l.progressBar(current, total, 20)
	full := fmt.Sprintf("%s %s (%d/%d)", bar, msg, current, total)
	if l.color {
		l.write(LevelInfo, " ", dim+full+reset)
	} else {
		l.write(LevelInfo, " ", full)
	}
}

// CacheHit logs a cache hit for an item.
func (l *Logger) CacheHit(item string) {
	if l.level > LevelDebug {
		return
	}
	msg := fmt.Sprintf("cache hit: %s", item)
	if l.color {
		l.write(LevelDebug, iconCache, dim+msg+reset)
	} else {
		l.write(LevelDebug, iconCache, msg)
	}
}

// CacheMiss logs a cache miss for an item.
func (l *Logger) CacheMiss(item string) {
	if l.level > LevelDebug {
		return
	}
	msg := fmt.Sprintf("cache miss: %s (calling agent)", item)
	l.write(LevelDebug, iconAgent, msg)
}

// CacheStore logs a successful cache store operation.
func (l *Logger) CacheStore(item string) {
	if l.level > LevelDebug {
		return
	}
	msg := fmt.Sprintf("cache store: %s", item)
	if l.color {
		l.write(LevelDebug, iconCache, dim+msg+reset)
	} else {
		l.write(LevelDebug, iconCache, msg)
	}
}

// CacheSummary logs aggregate cache statistics for a tool phase.
func (l *Logger) CacheSummary(tool string, hits, misses, skipped int) {
	total := hits + misses
	hitRate := 0.0
	if total > 0 {
		hitRate = float64(hits) / float64(total) * 100
	}
	msg := fmt.Sprintf("%s cache: %d hits, %d misses (%.0f%% hit rate), %d skipped",
		tool, hits, misses, hitRate, skipped)
	if l.color {
		l.write(LevelInfo, iconCache, cyan+msg+reset)
	} else {
		l.write(LevelInfo, iconCache, msg)
	}
}

// AgentCall logs an agent invocation with context.
func (l *Logger) AgentCall(tool, item string) {
	msg := fmt.Sprintf("%s → %s", tool, item)
	if l.color {
		l.write(LevelInfo, iconAgent, magenta+msg+reset)
	} else {
		l.write(LevelInfo, iconAgent, msg)
	}
}

// AgentResult logs the result of an agent call with timing.
func (l *Logger) AgentResult(item string, duration time.Duration, tokens int) {
	msg := fmt.Sprintf("%s completed (%s, %d tokens)", item, formatDuration(duration), tokens)
	if l.color {
		l.write(LevelDebug, iconTime, dim+msg+reset)
	} else {
		l.write(LevelDebug, iconTime, msg)
	}
}

// AgentRetry logs a validation retry.
func (l *Logger) AgentRetry(item string, attempt int, reason string) {
	msg := fmt.Sprintf("retry %d for %s: %s", attempt, item, reason)
	if l.color {
		l.write(LevelWarn, iconRetry, yellow+msg+reset)
	} else {
		l.write(LevelWarn, iconRetry, msg)
	}
}

// Skip logs a skipped item with reason.
func (l *Logger) Skip(item, reason string) {
	msg := fmt.Sprintf("skipping %s: %s", item, reason)
	if l.color {
		l.write(LevelWarn, iconSkip, yellow+msg+reset)
	} else {
		l.write(LevelWarn, iconSkip, msg)
	}
}

// Error logs an error.
func (l *Logger) Error(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if l.color {
		l.write(LevelError, iconFail, red+msg+reset)
	} else {
		l.write(LevelError, iconFail, msg)
	}
}

// Warn logs a warning.
func (l *Logger) Warn(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if l.color {
		l.write(LevelWarn, iconWarn, yellow+msg+reset)
	} else {
		l.write(LevelWarn, iconWarn, msg)
	}
}

// Info logs an informational message.
func (l *Logger) Info(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.write(LevelInfo, " ", msg)
}

// Debug logs a debug message.
func (l *Logger) Debug(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if l.color {
		l.write(LevelDebug, " ", dim+msg+reset)
	} else {
		l.write(LevelDebug, " ", msg)
	}
}

// Summary logs a final summary block.
func (l *Logger) Summary(totalDuration time.Duration, stats map[string]int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	separator := strings.Repeat("═", 50)
	if l.color {
		fmt.Fprintf(l.w, "\n%s%s%s\n", bold, separator, reset)
		fmt.Fprintf(l.w, "%s%s Scan Complete%s (%s)\n", bold+green, iconDone, reset, formatDuration(totalDuration))
		fmt.Fprintf(l.w, "%s%s%s\n", bold, separator, reset)
	} else {
		fmt.Fprintf(l.w, "\n%s\n", separator)
		fmt.Fprintf(l.w, "%s Scan Complete (%s)\n", iconDone, formatDuration(totalDuration))
		fmt.Fprintf(l.w, "%s\n", separator)
	}

	// Print stats
	for key, val := range stats {
		if l.color {
			fmt.Fprintf(l.w, "  %s%-25s%s %d\n", cyan, key+":", reset, val)
		} else {
			fmt.Fprintf(l.w, "  %-25s %d\n", key+":", val)
		}
	}
	fmt.Fprintln(l.w)
}

// --- Helpers ---

func (l *Logger) progressBar(current, total, width int) string {
	if total == 0 {
		return "[" + strings.Repeat(" ", width) + "]"
	}
	filled := (current * width) / total
	if filled > width {
		filled = width
	}
	empty := width - filled

	bar := "[" + strings.Repeat("█", filled) + strings.Repeat("░", empty) + "]"
	pct := (current * 100) / total

	if l.color {
		return fmt.Sprintf("%s%s%s %3d%%", green, bar, reset, pct)
	}
	return fmt.Sprintf("%s %3d%%", bar, pct)
}

func phaseIcon(phase int) string {
	switch phase {
	case 1:
		return iconDiscover
	case 2:
		return iconClone
	case 3:
		return iconAgent
	case 4:
		return iconAnalyze
	case 5:
		return iconMatch
	case 6:
		return iconReport
	default:
		return " "
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%ds", m, s)
}
