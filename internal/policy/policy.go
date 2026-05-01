package policy

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// RotationMode controls how an active log is rotated.
type RotationMode string

const (
	ModeRenameCreate RotationMode = "rename-create"
	ModeCopyTruncate  RotationMode = "copytruncate"
	ModeSignalReopen  RotationMode = "signal-reopen"
)

// Rule describes one rotation policy. It intentionally uses simple primitives
// so the CLI can later be backed by TOML/YAML without changing the executor.
type Rule struct {
	Name       string
	Path       string
	Pattern    string
	OldDir     string
	Mode       RotationMode
	Compress   bool
	Encrypt    bool
	Retention  int
	MaxAgeDays int
	MinSize    int64
	MaxSize    int64
	CreateMode os.FileMode
	PostRotate []string
	SIGHUPPid  int
}

type Policy struct {
	Rules []Rule
}

func DefaultRule() Rule {
	return Rule{
		Name:       "default",
		Path:       "/var/log/apps",
		Pattern:    "*.log",
		OldDir:     "old_logs",
		Mode:       ModeRenameCreate,
		Compress:   true,
		Retention:  14,
		MaxAgeDays: 1,
		CreateMode: 0640,
	}
}

// Load reads a small line-oriented policy format:
// [rule name]
// path=/var/log/myapp
// pattern=*.log
// mode=rename-create|copytruncate|signal-reopen
// retention=14
// max_age_days=1
// min_size=1048576
// compress=true
func Load(path string) (*Policy, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	p := &Policy{}
	current := DefaultRule()
	seenRule := false
	flush := func() {
		if seenRule {
			p.Rules = append(p.Rules, current)
		}
	}

	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			flush()
			current = DefaultRule()
			current.Name = strings.TrimSpace(strings.Trim(line, "[]"))
			seenRule = true
			continue
		}
		if !seenRule {
			seenRule = true
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("invalid policy line %q", line)
		}
		if err := apply(&current, strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), "\"'")); err != nil {
			return nil, err
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	flush()
	if len(p.Rules) == 0 {
		p.Rules = append(p.Rules, DefaultRule())
	}
	for i := range p.Rules {
		if err := Validate(p.Rules[i]); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func apply(r *Rule, key, value string) error {
	switch strings.ToLower(key) {
	case "name":
		r.Name = value
	case "path", "log_dir":
		r.Path = value
	case "pattern":
		r.Pattern = value
	case "old_dir", "olddir":
		r.OldDir = value
	case "mode":
		r.Mode = RotationMode(value)
	case "compress":
		r.Compress = parseBool(value)
	case "encrypt":
		r.Encrypt = parseBool(value)
	case "retention", "rotate":
		r.Retention = atoi(value)
	case "max_age_days":
		r.MaxAgeDays = atoi(value)
	case "min_size":
		r.MinSize = atoi64(value)
	case "max_size", "size":
		r.MaxSize = atoi64(value)
	case "create_mode":
		mode, err := strconv.ParseUint(value, 8, 32)
		if err != nil {
			return fmt.Errorf("invalid create_mode %q", value)
		}
		r.CreateMode = os.FileMode(mode)
	case "postrotate":
		if value != "" {
			r.PostRotate = append(r.PostRotate, value)
		}
	case "sighup_pid":
		r.SIGHUPPid = atoi(value)
	default:
		return fmt.Errorf("unknown policy key %q", key)
	}
	return nil
}

func Validate(r Rule) error {
	if r.Name == "" {
		return fmt.Errorf("rule name is required")
	}
	if r.Path == "" {
		return fmt.Errorf("rule %q: path is required", r.Name)
	}
	if !filepath.IsAbs(r.Path) {
		return fmt.Errorf("rule %q: path must be absolute", r.Name)
	}
	if r.Pattern == "" {
		return fmt.Errorf("rule %q: pattern is required", r.Name)
	}
	if _, err := filepath.Match(r.Pattern, "test.log"); err != nil {
		return fmt.Errorf("rule %q: invalid pattern: %w", r.Name, err)
	}
	switch r.Mode {
	case ModeRenameCreate, ModeCopyTruncate, ModeSignalReopen:
		return nil
	default:
		return fmt.Errorf("rule %q: invalid mode %q", r.Name, r.Mode)
	}
}

func ShouldRotate(info os.FileInfo, r Rule, now time.Time) bool {
	if info == nil || info.IsDir() || info.Size() == 0 {
		return false
	}
	if r.MaxSize > 0 && info.Size() >= r.MaxSize {
		return true
	}
	if r.MinSize > 0 && info.Size() < r.MinSize {
		return false
	}
	if r.MaxAgeDays > 0 && now.Sub(info.ModTime()) >= time.Duration(r.MaxAgeDays)*24*time.Hour {
		return true
	}
	return r.MaxSize == 0 && r.MaxAgeDays == 0
}

func parseBool(v string) bool {
	switch strings.ToLower(v) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func atoi(v string) int { n, _ := strconv.Atoi(v); return n }
func atoi64(v string) int64 { n, _ := strconv.ParseInt(v, 10, 64); return n }
