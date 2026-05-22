package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ============================================================
// Schedule parsing
// ============================================================

func TestParseInterval(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
		err  bool
	}{
		{"1h", time.Hour, false},
		{"30m", 30 * time.Minute, false},
		{"6h", 6 * time.Hour, false},
		{"24h", 24 * time.Hour, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"1h30m", 90 * time.Minute, false},
		{"bad", 0, true},
		{"-1h", 0, true},
		{"0h", 0, true},
		{"", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseInterval(tt.in)
			if (err != nil) != tt.err {
				t.Fatalf("parseInterval(%q) err=%v wantErr=%v", tt.in, err, tt.err)
			}
			if !tt.err && got != tt.want {
				t.Errorf("parseInterval(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsCronExpr(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"0 2 * * *", true},
		{"*/15 * * * *", true},
		{"@daily", true},
		{"@hourly", true},
		{"@weekly", true},
		{"@monthly", true},
		{"6h", false},
		{"30m", false},
		{"24h", false},
		{"1h30m", false},
	}
	for _, tt := range tests {
		if got := isCronExpr(tt.in); got != tt.want {
			t.Errorf("isCronExpr(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestParseCronField(t *testing.T) {
	tests := []struct {
		in     string
		lo, hi int
		want   []int
		err    bool
	}{
		{"2", 0, 23, []int{2}, false},
		{"0", 0, 59, []int{0}, false},
		{"*/15", 0, 59, []int{0, 15, 30, 45}, false},
		{"*/6", 0, 23, []int{0, 6, 12, 18}, false},
		{"1-3", 0, 59, []int{1, 2, 3}, false},
		{"1,5,10", 0, 59, []int{1, 5, 10}, false},
		{"0,30", 0, 59, []int{0, 30}, false},
		// errors
		{"bad", 0, 59, nil, true},
		{"100", 0, 59, nil, true},
		{"*/0", 0, 59, nil, true},
		{"5-2", 0, 59, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseCronField(tt.in, tt.lo, tt.hi)
			if (err != nil) != tt.err {
				t.Fatalf("parseCronField(%q) err=%v wantErr=%v", tt.in, err, tt.err)
			}
			if tt.want != nil {
				if len(got) != len(tt.want) {
					t.Fatalf("parseCronField(%q) = %v, want %v", tt.in, got, tt.want)
				}
				for i := range got {
					if got[i] != tt.want[i] {
						t.Errorf("parseCronField(%q)[%d] = %d, want %d", tt.in, i, got[i], tt.want[i])
					}
				}
			}
		})
	}
}

func TestCronNext(t *testing.T) {
	// Reference: 2024-01-15 10:30 Monday
	ref := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name  string
		expr  string
		wantH int
		wantM int
	}{
		{"daily 2am", "0 2 * * *", 2, 0},
		{"every 15 min", "*/15 * * * *", 10, 45},
		{"hourly", "@hourly", 11, 0},
		{"midnight", "@midnight", 0, 0},
		{"specific", "30 14 * * *", 14, 30},
		{"every 6h", "0 */6 * * *", 12, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := cronNext(tt.expr, ref)
			if err != nil {
				t.Fatalf("cronNext(%q): %v", tt.expr, err)
			}
			if got.Hour() != tt.wantH || got.Minute() != tt.wantM {
				t.Errorf("cronNext(%q) = %02d:%02d, want %02d:%02d",
					tt.expr, got.Hour(), got.Minute(), tt.wantH, tt.wantM)
			}
			if !got.After(ref) {
				t.Errorf("cronNext(%q) = %v, must be after ref %v", tt.expr, got, ref)
			}
		})
	}
}

func TestCronNextShorthands(t *testing.T) {
	ref := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	cases := []struct{ alias, equiv string }{
		{"@daily", "0 0 * * *"},
		{"@midnight", "0 0 * * *"},
		{"@weekly", "0 0 * * 0"},
		{"@monthly", "0 0 1 * *"},
		{"@hourly", "0 * * * *"},
	}
	for _, tt := range cases {
		t.Run(tt.alias, func(t *testing.T) {
			a, e1 := cronNext(tt.alias, ref)
			b, e2 := cronNext(tt.equiv, ref)
			if e1 != nil || e2 != nil {
				t.Fatalf("cron error: %v / %v", e1, e2)
			}
			if !a.Equal(b) {
				t.Errorf("%q → %v, %q → %v (should be equal)", tt.alias, a, tt.equiv, b)
			}
		})
	}
}

func TestCronNextBadExpr(t *testing.T) {
	ref := time.Now()
	bad := []string{"not valid", "* * *", "a b c d e", "99 * * * *"}
	for _, expr := range bad {
		if _, err := cronNext(expr, ref); err == nil {
			t.Errorf("cronNext(%q) expected error", expr)
		}
	}
}

func TestNextRunTime(t *testing.T) {
	ref := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)

	got, err := nextRunTime("6h", ref)
	if err != nil {
		t.Fatalf("interval: %v", err)
	}
	if got.Sub(ref) != 6*time.Hour {
		t.Errorf("6h: diff = %v, want 6h", got.Sub(ref))
	}

	got, err = nextRunTime("0 2 * * *", ref)
	if err != nil {
		t.Fatalf("cron: %v", err)
	}
	if got.Hour() != 2 || got.Minute() != 0 {
		t.Errorf("cron 0 2 * * *: got %02d:%02d, want 02:00", got.Hour(), got.Minute())
	}

	if _, err := nextRunTime("invalid schedule", ref); err == nil {
		t.Error("expected error for invalid schedule")
	}
}

// ============================================================
// Compression
// ============================================================

func TestCompressDecompressRoundtrip(t *testing.T) {
	original := []byte("2024-01-15 INFO test log entry\n" + strings.Repeat("log data ", 200))

	compressed, err := compressGzip(bytes.NewReader(original))
	if err != nil {
		t.Fatalf("compressGzip: %v", err)
	}
	if len(compressed) == 0 {
		t.Fatal("compressed output empty")
	}

	decompressed, err := decompressGzip(compressed)
	if err != nil {
		t.Fatalf("decompressGzip: %v", err)
	}
	if !bytes.Equal(decompressed, original) {
		t.Errorf("roundtrip mismatch: got %d bytes, want %d", len(decompressed), len(original))
	}
}

func TestCompressDecompressEmpty(t *testing.T) {
	compressed, err := compressGzip(bytes.NewReader([]byte{}))
	if err != nil {
		t.Fatalf("compressGzip(empty): %v", err)
	}
	recovered, err := decompressGzip(compressed)
	if err != nil {
		t.Fatalf("decompressGzip(empty): %v", err)
	}
	if len(recovered) != 0 {
		t.Errorf("expected empty, got %d bytes", len(recovered))
	}
}

func TestDecompressGzipBadInput(t *testing.T) {
	if _, err := decompressGzip([]byte("not gzip data")); err == nil {
		t.Error("expected error for invalid gzip input")
	}
}

// ============================================================
// Encryption
// ============================================================

func TestEncryptDecryptRoundtrip(t *testing.T) {
	plaintext := []byte("sensitive log content 1234567890")
	password := "test-password-xyz"

	ct, err := encryptData(plaintext, password)
	if err != nil {
		t.Fatalf("encryptData: %v", err)
	}
	if bytes.Equal(ct, plaintext) {
		t.Error("ciphertext equals plaintext")
	}

	got, err := decryptData(ct, password)
	if err != nil {
		t.Fatalf("decryptData: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Error("decrypted != original")
	}
}

func TestEncryptOutputNondeterministic(t *testing.T) {
	ct1, _ := encryptData([]byte("same data"), "pw")
	ct2, _ := encryptData([]byte("same data"), "pw")
	if bytes.Equal(ct1, ct2) {
		t.Error("two encryptions of same plaintext are identical — salt/nonce not random")
	}
}

func TestDecryptWrongPassword(t *testing.T) {
	ct, _ := encryptData([]byte("data"), "correct")
	if _, err := decryptData(ct, "wrong"); err == nil {
		t.Error("expected error for wrong password")
	}
}

func TestDecryptBadMagic(t *testing.T) {
	bad := make([]byte, 100)
	copy(bad, "BAAD")
	if _, err := decryptData(bad, "pw"); err == nil {
		t.Error("expected error for bad magic bytes")
	}
}

func TestDecryptTooShort(t *testing.T) {
	if _, err := decryptData([]byte("short"), "pw"); err == nil {
		t.Error("expected error for data too short")
	}
}

func TestCompressEncryptRoundtrip(t *testing.T) {
	original := []byte(strings.Repeat("log line content\n", 100))

	compressed, err := compressGzip(bytes.NewReader(original))
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	encrypted, err := encryptData(compressed, "pw")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	decrypted, err := decryptData(encrypted, "pw")
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	decompressed, err := decompressGzip(decrypted)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	if !bytes.Equal(decompressed, original) {
		t.Error("compress→encrypt→decrypt→decompress roundtrip failed")
	}
}

// ============================================================
// Utility functions
// ============================================================

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{1024 * 1024, "1.00 MB"},
		{1024 * 1024 * 1024, "1.00 GB"},
		{1024 * 1024 * 1024 * 1024, "1.00 TB"},
	}
	for _, tt := range tests {
		if got := formatSize(tt.bytes); got != tt.want {
			t.Errorf("formatSize(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

func TestMatchesHash(t *testing.T) {
	// sha256("password")
	hash := "5e884898da28047151d0e56f8dc6292773603d0d6aabbdd62a11ef721d1542d8"
	if !matchesHash("password", hash) {
		t.Error("expected true for correct password")
	}
	if matchesHash("wrong", hash) {
		t.Error("expected false for wrong password")
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"error", LogLevelError}, {"ERROR", LogLevelError}, {"0", LogLevelError},
		{"info", LogLevelInfo}, {"INFO", LogLevelInfo}, {"1", LogLevelInfo},
		{"debug", LogLevelDebug}, {"DEBUG", LogLevelDebug}, {"2", LogLevelDebug},
		{"unknown", LogLevelInfo}, {"", LogLevelInfo},
	}
	for _, tt := range tests {
		if got := parseLogLevel(tt.in); got != tt.want {
			t.Errorf("parseLogLevel(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestMaskPassword(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"ab", "****"},
		{"abc", "a*c"},
		{"password", "p******d"},
	}
	for _, tt := range tests {
		if got := maskPassword(tt.in); got != tt.want {
			t.Errorf("maskPassword(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCronRange(t *testing.T) {
	got := cronRange(0, 10, 3)
	want := []int{0, 3, 6, 9}
	if len(got) != len(want) {
		t.Fatalf("cronRange(0,10,3) = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d] got %d want %d", i, got[i], want[i])
		}
	}
}

func TestIntIn(t *testing.T) {
	s := []int{1, 3, 5, 7}
	for _, v := range []int{1, 3, 5, 7} {
		if !intIn(s, v) {
			t.Errorf("intIn: %d should be found", v)
		}
	}
	for _, v := range []int{0, 2, 4, 8} {
		if intIn(s, v) {
			t.Errorf("intIn: %d should not be found", v)
		}
	}
}

// ============================================================
// Config helpers
// ============================================================

func TestGetConfigDefault(t *testing.T) {
	m := map[string]string{"K": "val", "EMPTY": ""}
	if got := getConfigDefault(m, "K", "def"); got != "val" {
		t.Errorf("got %q", got)
	}
	if got := getConfigDefault(m, "MISSING", "def"); got != "def" {
		t.Errorf("got %q", got)
	}
	if got := getConfigDefault(m, "EMPTY", "def"); got != "def" {
		t.Errorf("empty value should fall back to default, got %q", got)
	}
}

func TestGetConfigDefaultInt(t *testing.T) {
	m := map[string]string{"N": "42", "BAD": "notint", "NEG": "-5"}
	if got := getConfigDefaultInt(m, "N", 0); got != 42 {
		t.Errorf("got %d", got)
	}
	if got := getConfigDefaultInt(m, "MISSING", 10); got != 10 {
		t.Errorf("got %d", got)
	}
	if got := getConfigDefaultInt(m, "BAD", 99); got != 99 {
		t.Errorf("bad int should return default, got %d", got)
	}
	if got := getConfigDefaultInt(m, "NEG", 0); got != -5 {
		t.Errorf("negative int: got %d", got)
	}
}

func TestGetConfigDefaultBool(t *testing.T) {
	m := map[string]string{
		"T1": "true", "T2": "yes", "T3": "1",
		"F1": "false", "F2": "no", "F3": "0",
	}
	for _, k := range []string{"T1", "T2", "T3"} {
		if !getConfigDefaultBool(m, k, false) {
			t.Errorf("key %s: expected true", k)
		}
	}
	for _, k := range []string{"F1", "F2", "F3"} {
		if getConfigDefaultBool(m, k, true) {
			t.Errorf("key %s: expected false", k)
		}
	}
	if !getConfigDefaultBool(m, "MISSING", true) {
		t.Error("missing key should use default=true")
	}
}

func TestBuildConfigDefaults(t *testing.T) {
	cfg := buildConfig(map[string]string{})
	if cfg.LogDir != defaultDir {
		t.Errorf("LogDir = %q, want %q", cfg.LogDir, defaultDir)
	}
	if cfg.Pattern != "*.log" {
		t.Errorf("Pattern = %q", cfg.Pattern)
	}
	if cfg.ParallelJobs != defaultJobs {
		t.Errorf("ParallelJobs = %d, want %d", cfg.ParallelJobs, defaultJobs)
	}
	if cfg.DiskCriticalPct != defaultDiskCriticalPct {
		t.Errorf("DiskCriticalPct = %d", cfg.DiskCriticalPct)
	}
	if cfg.DiskMinFreeMB != int64(defaultDiskMinFreeMB) {
		t.Errorf("DiskMinFreeMB = %d", cfg.DiskMinFreeMB)
	}
	if cfg.CloudDays != 1 {
		t.Errorf("CloudDays = %d, want 1", cfg.CloudDays)
	}
}

func TestBuildConfigOverrides(t *testing.T) {
	cfg := buildConfig(map[string]string{
		"LOG_DIR":                "/var/log/test",
		"PATTERN":                "*.gz",
		"PARALLEL_JOBS":          "8",
		"DISK_CRITICAL_PERCENT":  "85",
		"DISK_MIN_FREE_MB":       "500",
		"SCHEDULE":               "0 3 * * *",
		"CLOUD_PROVIDER":         "aws",
		"CLOUD_DESTINATION":      "s3://bucket/prefix",
		"CLOUD_BACKUP_ON_PANIC":  "true",
	})
	if cfg.LogDir != "/var/log/test" {
		t.Errorf("LogDir = %q", cfg.LogDir)
	}
	if cfg.Pattern != "*.gz" {
		t.Errorf("Pattern = %q", cfg.Pattern)
	}
	if cfg.ParallelJobs != 8 {
		t.Errorf("ParallelJobs = %d", cfg.ParallelJobs)
	}
	if cfg.DiskCriticalPct != 85 {
		t.Errorf("DiskCriticalPct = %d", cfg.DiskCriticalPct)
	}
	if cfg.DiskMinFreeMB != 500 {
		t.Errorf("DiskMinFreeMB = %d", cfg.DiskMinFreeMB)
	}
	if cfg.Schedule != "0 3 * * *" {
		t.Errorf("Schedule = %q", cfg.Schedule)
	}
	if cfg.CloudProvider != "aws" {
		t.Errorf("CloudProvider = %q", cfg.CloudProvider)
	}
	if !cfg.CloudOnPanic {
		t.Error("CloudOnPanic should be true")
	}
}

func TestBuildConfigCloudSourceDefault(t *testing.T) {
	// When no OLD_LOGS_DIR and no CLOUD_SOURCE, source defaults to LogDir/old_logs
	cfg := buildConfig(map[string]string{"LOG_DIR": "/var/log/app"})
	if cfg.CloudSource != "/var/log/app/old_logs" {
		t.Errorf("CloudSource = %q, want /var/log/app/old_logs", cfg.CloudSource)
	}

	// When OLD_LOGS_DIR is set, cloud source inherits it
	cfg2 := buildConfig(map[string]string{
		"LOG_DIR":     "/var/log/app",
		"OLD_LOGS_DIR": "/mnt/logs/old",
	})
	if cfg2.CloudSource != "/mnt/logs/old" {
		t.Errorf("CloudSource = %q, want /mnt/logs/old", cfg2.CloudSource)
	}
}

// ============================================================
// Disk stats
// ============================================================

func TestDiskStats(t *testing.T) {
	total, free, pct, err := diskStats("/tmp")
	if err != nil {
		t.Fatalf("diskStats(/tmp): %v", err)
	}
	if total <= 0 {
		t.Error("total should be > 0")
	}
	if free < 0 {
		t.Error("free should be >= 0")
	}
	if pct < 0 || pct > 100 {
		t.Errorf("pct = %.2f, want 0-100", pct)
	}
	if free > total {
		t.Errorf("free (%d) > total (%d)", free, total)
	}
}

func TestDiskStatsBadPath(t *testing.T) {
	if _, _, _, err := diskStats("/nonexistent/path/xyz"); err == nil {
		t.Error("expected error for nonexistent path")
	}
}

// ============================================================
// File discovery
// ============================================================

func TestFindLogFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"app.log", "access.log", "error.log", "other.txt", "debug.log"} {
		os.WriteFile(filepath.Join(dir, name), []byte("content"), 0644)
	}
	files := findLogFiles(dir, "*.log", nil)
	if len(files) != 4 {
		t.Errorf("found %d files, want 4", len(files))
	}
}

func TestFindLogFilesExclude(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"app.log", "access.log", "debug.log"} {
		os.WriteFile(filepath.Join(dir, name), []byte("content"), 0644)
	}
	files := findLogFiles(dir, "*.log", []string{"debug.log"})
	if len(files) != 2 {
		t.Errorf("found %d files, want 2 (debug.log excluded)", len(files))
	}
	for _, f := range files {
		if filepath.Base(f.path) == "debug.log" {
			t.Error("excluded file debug.log appears in results")
		}
	}
}

func TestFindLogFilesNoMatch(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "other.txt"), []byte("x"), 0644)
	files := findLogFiles(dir, "*.log", nil)
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

func TestFindLogFilesSortedBySize(t *testing.T) {
	dir := t.TempDir()
	// Write files of different sizes
	sizes := []int{100, 50, 200, 10}
	for i, sz := range sizes {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("app%d.log", i)), bytes.Repeat([]byte("x"), sz), 0644)
	}
	files := findLogFiles(dir, "*.log", nil)
	for i := 1; i < len(files); i++ {
		if files[i].size < files[i-1].size {
			t.Errorf("files not sorted by size: [%d]=%d > [%d]=%d", i-1, files[i-1].size, i, files[i].size)
		}
	}
}

// ============================================================
// Rotation integration tests
// ============================================================

func makeTestCfg(t *testing.T, dir string) *Config {
	t.Helper()
	oldDir := filepath.Join(dir, "old")
	cfg := buildConfig(map[string]string{
		"LOG_DIR":     dir,
		"OLD_LOGS_DIR": oldDir,
	})
	cfg.DateSuffix = "20240115"
	cfg.BackupDate = "20240115"
	cfg.DiskMinFreeMB = 0 // disable disk check in tests
	return cfg
}

func TestRotateLogFileBasic(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "app.log")
	content := []byte("2024-01-15 10:00:00 INFO test log entry\n" + strings.Repeat("data", 100))
	if err := os.WriteFile(logPath, content, 0644); err != nil {
		t.Fatal(err)
	}

	cfg := makeTestCfg(t, dir)
	rotateLogFile(logPath, cfg)

	// Original must be truncated
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("original file missing: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("original not truncated: size=%d", info.Size())
	}

	// Archive must exist and decompress to original content
	archivePath := filepath.Join(dir, "old", "20240115", "app.log.20240115.gz")
	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("archive not found at %s: %v", archivePath, err)
	}
	recovered, err := decompressGzip(data)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	if !bytes.Equal(recovered, content) {
		t.Error("recovered content != original")
	}
}

func TestRotateLogFileEncrypted(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "secure.log")
	content := []byte("sensitive data\n")
	os.WriteFile(logPath, content, 0644)

	password := "test-encrypt-pw"
	cfg := makeTestCfg(t, dir)
	cfg.Encrypt = true
	cfg.EncryptPassword = password
	// Reset cachedPassword to avoid interference from other tests
	passwordMu.Lock()
	cachedPassword = ""
	passwordMu.Unlock()

	rotateLogFile(logPath, cfg)

	archivePath := filepath.Join(dir, "old", "20240115", "secure.log.20240115.gz.enc")
	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("encrypted archive not found: %v", err)
	}

	compressed, err := decryptData(data, password)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	recovered, err := decompressGzip(compressed)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	if !bytes.Equal(recovered, content) {
		t.Error("encrypted roundtrip failed")
	}
}

func TestRotateLogFileSkipsEmpty(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "empty.log")
	os.WriteFile(logPath, []byte{}, 0644)

	rotateLogFile(logPath, makeTestCfg(t, dir))

	archiveDir := filepath.Join(dir, "old")
	if _, err := os.Stat(archiveDir); !os.IsNotExist(err) {
		t.Error("archive dir should not exist for empty file")
	}
}

func TestRotateLogFileDryRun(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")
	content := []byte("log content")
	os.WriteFile(logPath, content, 0644)

	cfg := makeTestCfg(t, dir)
	cfg.DryRun = true
	rotateLogFile(logPath, cfg)

	// File must be unmodified
	info, _ := os.Stat(logPath)
	if info.Size() == 0 {
		t.Error("dry-run must not truncate original")
	}
	// No archive
	if _, err := os.Stat(filepath.Join(dir, "old")); !os.IsNotExist(err) {
		t.Error("dry-run must not create archive")
	}
}

func TestRotateLogFileAlreadyRotated(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "app.log")
	content := []byte("log content")
	os.WriteFile(logPath, content, 0644)

	cfg := makeTestCfg(t, dir)

	// First rotation
	rotateLogFile(logPath, cfg)

	// Re-fill the file
	os.WriteFile(logPath, content, 0644)

	// Second rotation should skip (archive already exists)
	rotateLogFile(logPath, cfg)

	// Original should still have content (second rotation skipped)
	info, _ := os.Stat(logPath)
	if info.Size() == 0 {
		t.Error("second rotation should be skipped when archive exists")
	}
}

func TestRotateLogFileDiskGuard(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "app.log")
	os.WriteFile(logPath, []byte("data to rotate"), 0644)

	cfg := makeTestCfg(t, dir)
	cfg.DiskMinFreeMB = 999_999_999 // impossibly large — always triggers skip

	rotateLogFile(logPath, cfg)

	// Original must NOT be truncated — disk guard returned early
	info, _ := os.Stat(logPath)
	if info.Size() == 0 {
		t.Error("disk guard should prevent truncation when free space < DiskMinFreeMB")
	}
	// No archive
	archivePath := filepath.Join(dir, "old", "20240115", "app.log.20240115.gz")
	if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
		t.Error("disk guard should prevent archive creation")
	}
}

func TestRotateParallelMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	var files []fileInfo
	for i := range 5 {
		name := fmt.Sprintf("app%d.log", i)
		path := filepath.Join(dir, name)
		content := bytes.Repeat([]byte("x"), 100+i*50)
		os.WriteFile(path, content, 0644)
		info, _ := os.Stat(path)
		files = append(files, fileInfo{path: path, size: info.Size()})
	}

	cfg := makeTestCfg(t, dir)
	cfg.ParallelJobs = 3
	cfg.Parallel = true

	rotateParallel(files, cfg)

	for _, f := range files {
		info, err := os.Stat(f.path)
		if err != nil {
			t.Errorf("file %s missing: %v", f.path, err)
			continue
		}
		if info.Size() != 0 {
			t.Errorf("file %s not truncated after parallel rotation", f.path)
		}
	}
}

func TestRotateSequential(t *testing.T) {
	dir := t.TempDir()
	var files []fileInfo
	for i := range 3 {
		path := filepath.Join(dir, fmt.Sprintf("s%d.log", i))
		os.WriteFile(path, []byte("seq content"), 0644)
		info, _ := os.Stat(path)
		files = append(files, fileInfo{path: path, size: info.Size()})
	}
	rotateSequential(files, makeTestCfg(t, dir))
	for _, f := range files {
		info, _ := os.Stat(f.path)
		if info.Size() != 0 {
			t.Errorf("file %s not truncated", f.path)
		}
	}
}

func TestRotateLogFileArchivePermissions(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "perm.log")
	// Write with setuid bit to verify it's stripped in the archive
	os.WriteFile(logPath, []byte("perm test"), 0644)
	// Set file permissions including execute bit
	os.Chmod(logPath, 0755)

	cfg := makeTestCfg(t, dir)
	rotateLogFile(logPath, cfg)

	archivePath := filepath.Join(dir, "old", "20240115", "perm.log.20240115.gz")
	info, err := os.Stat(archivePath)
	if err != nil {
		t.Fatalf("archive not found: %v", err)
	}
	mode := info.Mode()
	// Execute bit must be stripped from archive
	if mode&0111 != 0 {
		t.Errorf("archive has execute bits set: %v — should be stripped", mode)
	}
}
