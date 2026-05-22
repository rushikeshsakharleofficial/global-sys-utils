package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/term"
)

const (
	version         = "2.1.15"
	defaultDir      = "/var/log/apps"
	defaultJobs     = 4
	mainConfigFile  = "/etc/global-sys-utils/global.conf"
	configDropinDir = "/etc/global-sys-utils/global.conf.d"
	defaultLogFile  = "/var/log/global-sys-utils/global-logrotate.log"

	// Encryption constants
	saltSize   = 32
	nonceSize  = 12
	keySize    = 32 // AES-256
	iterations = 100000

	// Daemon defaults
	defaultDiskCriticalPct = 90   // trigger emergency rotation when disk reaches this %
	defaultDiskMinFreeMB   = 200  // refuse to write archive if less free MB than this
	defaultDiskCheckSec    = 60   // seconds between disk checks
	defaultPIDFile         = "/run/global-logrotate.pid"
)

// Log levels
const (
	LogLevelError = iota
	LogLevelInfo
	LogLevelDebug
)

// encryptMagic identifies our encrypted file format: MAGIC(4)+SALT(32)+NONCE(12)+CIPHERTEXT
const encryptMagicStr = "GLRE"

var encryptMagic = []byte(encryptMagicStr)

// Logger handles application logging
type Logger struct {
	level    int
	file     *os.File
	filePath string
	mu       sync.Mutex
}

var logger *Logger
var cachedPassword string
var passwordMu sync.Mutex

type Config struct {
	LogDir          string
	Pattern         string
	DateSuffix      string
	DateFormat      string
	OldLogsDir      string
	ExcludeFile     string
	DryRun          bool
	Parallel        bool
	ParallelJobs    int
	CustomPath      bool
	Encrypt         bool
	EncryptPassword string
	EncryptPassHash string
	ReadFile        string
	PassGen         bool
	PassReset       bool
	// BackupDate is computed once at startup so all files in a run use the same date.
	BackupDate string
	// Logging config
	LogFile  string
	LogLevel int
	// Daemon / scheduling
	JobName    string // human label derived from conf.d filename
	Daemon     bool
	DaemonOnce bool   // run all jobs once then exit (cron/systemd-timer use case)
	Schedule   string // cron expression or interval string (e.g. "6h", "0 2 * * *")
	PIDFile    string
	// Disk safety
	DiskCriticalPct int   // % disk used — triggers immediate rotation
	DiskMinFreeMB   int64 // minimum free MB required to write an archive
	DiskCheckSec    int   // interval between disk checks in daemon mode
	// Cloud backup integration (triggered by daemon after rotation or in panic mode)
	CloudProvider       string // "aws" | "gcp" | "" (empty = disabled)
	CloudSource         string // local directory to backup (defaults to OldLogsDir or LogDir/old_logs)
	CloudDestination    string // s3://bucket/prefix or gs://bucket/prefix
	CloudDays           int    // only backup files older than N days
	CloudParallel       int    // concurrent uploads
	CloudTimeout        int    // per-operation timeout in seconds
	CloudAWSProfile     string
	CloudAWSRegion      string
	CloudGCPProject     string
	CloudGCPCredentials string
	CloudOnSchedule     bool // run cloud backup after every scheduled rotation
	CloudOnPanic        bool // run cloud backup when disk reaches DISK_CRITICAL_PERCENT
}

// initLogger initializes the global logger
func initLogger(logFile string, level int) error {
	logDir := filepath.Dir(logFile)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	logger = &Logger{
		level:    level,
		file:     file,
		filePath: logFile,
	}

	return nil
}

// closeLogger closes the log file
func closeLogger() {
	if logger != nil && logger.file != nil {
		logger.file.Close()
	}
}

// logWrite writes a log entry. String formatting happens outside the mutex to minimize lock hold time.
func logWrite(level int, format string, args ...interface{}) {
	if logger == nil || level > logger.level {
		return
	}

	levelStr := "INFO"
	switch level {
	case LogLevelError:
		levelStr = "ERROR"
	case LogLevelDebug:
		levelStr = "DEBUG"
	}

	line := fmt.Sprintf("[%s] [%s] %s\n",
		time.Now().Format("2006-01-02 15:04:05"),
		levelStr,
		fmt.Sprintf(format, args...),
	)

	logger.mu.Lock()
	if _, err := logger.file.WriteString(line); err != nil {
		fmt.Fprint(os.Stderr, line) // disk full or closed — fall back to stderr
	}
	logger.mu.Unlock()
}

// Convenience logging functions
func logError(format string, args ...interface{}) {
	logWrite(LogLevelError, format, args...)
}

func logInfo(format string, args ...interface{}) {
	logWrite(LogLevelInfo, format, args...)
}

func logDebug(format string, args ...interface{}) {
	logWrite(LogLevelDebug, format, args...)
}

// parseLogLevel converts string log level to int
func parseLogLevel(level string) int {
	switch strings.ToLower(level) {
	case "error", "0":
		return LogLevelError
	case "info", "1":
		return LogLevelInfo
	case "debug", "2":
		return LogLevelDebug
	default:
		return LogLevelInfo
	}
}

// ============================================================
// Disk stats
// ============================================================

// diskStats returns usage info for the filesystem containing path.
func diskStats(path string) (totalMB, freeMB int64, usedPct float64, err error) {
	var st syscall.Statfs_t
	if err = syscall.Statfs(path, &st); err != nil {
		return 0, 0, 0, fmt.Errorf("statfs %s: %w", path, err)
	}
	total := int64(st.Blocks) * int64(st.Bsize)
	free := int64(st.Bavail) * int64(st.Bsize)
	totalMB = total / (1024 * 1024)
	freeMB = free / (1024 * 1024)
	if total > 0 {
		usedPct = float64(total-free) / float64(total) * 100
	}
	return
}

// ============================================================
// Schedule parsing — cron expressions and interval strings
// ============================================================

// isCronExpr returns true when s looks like a 5-field cron expression or @shorthand.
func isCronExpr(s string) bool {
	return strings.HasPrefix(s, "@") || len(strings.Fields(s)) == 5
}

// parseInterval parses strings like "30m", "6h", "24h", "7d".
func parseInterval(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("invalid interval %q", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid interval %q: %w", s, err)
	}
	return d, nil
}

// nextRunTime returns the next scheduled time after from.
func nextRunTime(schedule string, from time.Time) (time.Time, error) {
	if isCronExpr(schedule) {
		return cronNext(schedule, from)
	}
	d, err := parseInterval(schedule)
	if err != nil {
		return time.Time{}, err
	}
	return from.Add(d), nil
}

// cronNext computes the next time after from that matches the cron expression.
// Supported: *, */n, n, n-m, n,m,o — for all five fields (min hour dom month dow).
// Shorthands: @hourly @daily @midnight @weekly @monthly.
func cronNext(expr string, from time.Time) (time.Time, error) {
	expr = strings.TrimSpace(expr)
	switch expr {
	case "@hourly":
		expr = "0 * * * *"
	case "@daily", "@midnight":
		expr = "0 0 * * *"
	case "@weekly":
		expr = "0 0 * * 0"
	case "@monthly":
		expr = "0 0 1 * *"
	}
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return time.Time{}, fmt.Errorf("cron expression must have 5 fields: %q", expr)
	}
	minutes, err := parseCronField(fields[0], 0, 59)
	if err != nil {
		return time.Time{}, fmt.Errorf("cron minute: %w", err)
	}
	hours, err := parseCronField(fields[1], 0, 23)
	if err != nil {
		return time.Time{}, fmt.Errorf("cron hour: %w", err)
	}
	doms, err := parseCronField(fields[2], 1, 31)
	if err != nil {
		return time.Time{}, fmt.Errorf("cron dom: %w", err)
	}
	months, err := parseCronField(fields[3], 1, 12)
	if err != nil {
		return time.Time{}, fmt.Errorf("cron month: %w", err)
	}
	dows, err := parseCronField(fields[4], 0, 6)
	if err != nil {
		return time.Time{}, fmt.Errorf("cron dow: %w", err)
	}
	// Walk minute-by-minute from (from+1m) up to ~4 years out.
	t := from.Truncate(time.Minute).Add(time.Minute)
	for range 2 * 365 * 24 * 60 {
		if intIn(months, int(t.Month())) &&
			(intIn(doms, t.Day()) || intIn(dows, int(t.Weekday()))) &&
			intIn(hours, t.Hour()) &&
			intIn(minutes, t.Minute()) {
			return t, nil
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}, fmt.Errorf("no next run found for cron %q", expr)
}

func parseCronField(s string, lo, hi int) ([]int, error) {
	var out []int
	for _, part := range strings.Split(s, ",") {
		vals, err := parseCronPart(part, lo, hi)
		if err != nil {
			return nil, err
		}
		out = append(out, vals...)
	}
	// deduplicate + sort
	seen := make(map[int]bool, len(out))
	var uniq []int
	for _, v := range out {
		if !seen[v] {
			seen[v] = true
			uniq = append(uniq, v)
		}
	}
	sort.Ints(uniq)
	return uniq, nil
}

func parseCronPart(s string, lo, hi int) ([]int, error) {
	if s == "*" {
		return cronRange(lo, hi, 1), nil
	}
	if strings.HasPrefix(s, "*/") {
		step, err := strconv.Atoi(s[2:])
		if err != nil || step <= 0 {
			return nil, fmt.Errorf("invalid step %q", s)
		}
		return cronRange(lo, hi, step), nil
	}
	if idx := strings.Index(s, "-"); idx > 0 {
		a, err1 := strconv.Atoi(s[:idx])
		b, err2 := strconv.Atoi(s[idx+1:])
		if err1 != nil || err2 != nil || a < lo || b > hi || a > b {
			return nil, fmt.Errorf("invalid range %q (must be %d-%d)", s, lo, hi)
		}
		return cronRange(a, b, 1), nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < lo || n > hi {
		return nil, fmt.Errorf("invalid value %q (must be %d-%d)", s, lo, hi)
	}
	return []int{n}, nil
}

func cronRange(lo, hi, step int) []int {
	r := make([]int, 0, (hi-lo)/step+1)
	for i := lo; i <= hi; i += step {
		r = append(r, i)
	}
	return r
}

func intIn(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// ============================================================
// PID file
// ============================================================

func writePIDFile(path string) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0644)
}

func removePIDFile(path string) {
	if path != "" {
		os.Remove(path) //nolint:errcheck
	}
}

// ============================================================
// Multi-job config loading for daemon mode
// ============================================================

// buildConfig converts a merged key-value map into a *Config with all defaults applied.
// Used both by parseFlags (for single-run mode) and loadJobConfigs (for daemon mode).
func buildConfig(fc map[string]string) *Config {
	cfg := &Config{
		LogDir:          getConfigDefault(fc, "LOG_DIR", defaultDir),
		Pattern:         getConfigDefault(fc, "PATTERN", "*.log"),
		ParallelJobs:    getConfigDefaultInt(fc, "PARALLEL_JOBS", defaultJobs),
		OldLogsDir:      getConfigDefault(fc, "OLD_LOGS_DIR", ""),
		ExcludeFile:     getConfigDefault(fc, "EXCLUDE_FILE", ""),
		DateFormat:      getConfigDefault(fc, "DATE_FORMAT", "date"),
		DryRun:          getConfigDefaultBool(fc, "DRY_RUN", false),
		Encrypt:         getConfigDefaultBool(fc, "ENCRYPT", false),
		EncryptPassword: getConfigDefault(fc, "ENCRYPT_PASSWORD", ""),
		EncryptPassHash: getConfigDefault(fc, "ENCRYPT_PASSWORD_HASH", ""),
		LogFile:         getConfigDefault(fc, "LOG_FILE", defaultLogFile),
		LogLevel:        parseLogLevel(getConfigDefault(fc, "LOG_LEVEL", "info")),
		Schedule:        getConfigDefault(fc, "SCHEDULE", ""),
		PIDFile:         getConfigDefault(fc, "PID_FILE", defaultPIDFile),
		DiskCriticalPct: getConfigDefaultInt(fc, "DISK_CRITICAL_PERCENT", defaultDiskCriticalPct),
		DiskMinFreeMB:   int64(getConfigDefaultInt(fc, "DISK_MIN_FREE_MB", defaultDiskMinFreeMB)),
		DiskCheckSec:    getConfigDefaultInt(fc, "DISK_CHECK_INTERVAL", defaultDiskCheckSec),
		// Cloud backup
		CloudProvider:       getConfigDefault(fc, "CLOUD_PROVIDER", ""),
		CloudSource:         getConfigDefault(fc, "CLOUD_SOURCE", ""),
		CloudDestination:    getConfigDefault(fc, "CLOUD_DESTINATION", ""),
		CloudDays:           getConfigDefaultInt(fc, "CLOUD_DAYS", 1),
		CloudParallel:       getConfigDefaultInt(fc, "CLOUD_PARALLEL", 4),
		CloudTimeout:        getConfigDefaultInt(fc, "CLOUD_TIMEOUT", 300),
		CloudAWSProfile:     getConfigDefault(fc, "CLOUD_AWS_PROFILE", ""),
		CloudAWSRegion:      getConfigDefault(fc, "CLOUD_AWS_REGION", ""),
		CloudGCPProject:     getConfigDefault(fc, "CLOUD_GCP_PROJECT", ""),
		CloudGCPCredentials: getConfigDefault(fc, "CLOUD_GCP_CREDENTIALS", ""),
		CloudOnSchedule:     getConfigDefaultBool(fc, "CLOUD_BACKUP_ON_SCHEDULE", false),
		CloudOnPanic:        getConfigDefaultBool(fc, "CLOUD_BACKUP_ON_PANIC", false),
	}
	cfg.Parallel = cfg.ParallelJobs > 1
	cfg.LogDir = strings.TrimSuffix(cfg.LogDir, "/")
	now := time.Now()
	cfg.DateSuffix = now.Format("20060102")
	cfg.BackupDate = cfg.DateSuffix
	// Default cloud source to the old_logs directory for this job.
	if cfg.CloudSource == "" {
		if cfg.OldLogsDir != "" {
			cfg.CloudSource = cfg.OldLogsDir
		} else {
			cfg.CloudSource = cfg.LogDir + "/old_logs"
		}
	}
	return cfg
}

// loadJobConfigs loads global.conf as defaults, then each conf.d/*.conf file as an
// independent rotation job that inherits those defaults.
func loadJobConfigs() []*Config {
	baseFC := make(map[string]string)
	loadConfigFile(mainConfigFile, baseFC)

	var jobs []*Config

	// The base config itself is a job if it has a schedule.
	base := buildConfig(baseFC)
	base.JobName = "global"
	if base.Schedule != "" {
		jobs = append(jobs, base)
	}

	files, _ := filepath.Glob(filepath.Join(configDropinDir, "*.conf"))
	sort.Strings(files)
	for _, f := range files {
		fc := make(map[string]string, len(baseFC))
		for k, v := range baseFC {
			fc[k] = v
		}
		loadConfigFile(f, fc)
		job := buildConfig(fc)
		job.JobName = strings.TrimSuffix(filepath.Base(f), ".conf")
		jobs = append(jobs, job)
	}
	return jobs
}

// ============================================================
// Cloud backup integration
// ============================================================

// runCloudBackup invokes the appropriate cloud backup script as a subprocess.
// panic=true means we're in emergency mode (disk critical); panic=false means post-schedule.
// Manual use of global-aws-backup / global-gcp-backup CLI is unaffected — those tools
// remain fully independent and callable directly by the user at any time.
func runCloudBackup(cfg *Config, emergency bool) {
	if cfg.CloudProvider == "" || cfg.CloudDestination == "" {
		return
	}
	if emergency && !cfg.CloudOnPanic {
		return
	}
	if !emergency && !cfg.CloudOnSchedule {
		return
	}

	var prog string
	switch strings.ToLower(cfg.CloudProvider) {
	case "aws":
		prog = "global-aws-backup"
	case "gcp":
		prog = "global-gcp-backup"
	default:
		logError("Job [%s]: unknown CLOUD_PROVIDER %q (must be aws or gcp)", cfg.JobName, cfg.CloudProvider)
		return
	}

	args := []string{
		"--source", cfg.CloudSource,
		"--destination", cfg.CloudDestination,
		"--days", strconv.Itoa(cfg.CloudDays),
		"--parallel", strconv.Itoa(cfg.CloudParallel),
		"--timeout", strconv.Itoa(cfg.CloudTimeout),
	}
	if cfg.CloudAWSProfile != "" {
		args = append(args, "--profile", cfg.CloudAWSProfile)
	}
	if cfg.CloudAWSRegion != "" {
		args = append(args, "--region", cfg.CloudAWSRegion)
	}
	if cfg.CloudGCPProject != "" {
		args = append(args, "--project", cfg.CloudGCPProject)
	}
	if cfg.CloudGCPCredentials != "" {
		args = append(args, "--credentials", cfg.CloudGCPCredentials)
	}

	mode := "scheduled"
	if emergency {
		mode = "PANIC"
	}
	logInfo("Job [%s]: starting %s cloud backup (%s) → %s", cfg.JobName, mode, prog, cfg.CloudDestination)

	cmd := exec.Command(prog, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		logError("Job [%s]: cloud backup failed: %v", cfg.JobName, err)
	} else {
		logInfo("Job [%s]: cloud backup completed", cfg.JobName)
	}
}

// ============================================================
// Daemon runner
// ============================================================

type daemonJob struct {
	cfg     *Config
	nextRun time.Time
}

func runDaemon(jobs []*Config, once bool) {
	if len(jobs) == 0 {
		fmt.Fprintln(os.Stderr, "Error: no rotation jobs found in config files")
		os.Exit(1)
	}

	if err := writePIDFile(jobs[0].PIDFile); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write PID file %s: %v\n", jobs[0].PIDFile, err)
	}
	defer removePIDFile(jobs[0].PIDFile)

	logInfo("global-logrotate daemon v%s starting with %d job(s)", version, len(jobs))

	// Validate schedules and compute initial next-run times.
	djobs := make([]*daemonJob, 0, len(jobs))
	for _, cfg := range jobs {
		if cfg.Schedule == "" {
			logInfo("Job [%s] has no SCHEDULE — skipping in daemon mode", cfg.JobName)
			continue
		}
		if _, err := nextRunTime(cfg.Schedule, time.Now()); err != nil {
			logError("Invalid SCHEDULE %q for job [%s]: %v", cfg.Schedule, cfg.JobName, err)
			continue
		}
		nr, _ := nextRunTime(cfg.Schedule, time.Now())
		djobs = append(djobs, &daemonJob{cfg: cfg, nextRun: nr})
		logInfo("Job [%s] dir=%s  schedule=%q  next=%s",
			cfg.JobName, cfg.LogDir, cfg.Schedule, nr.Format("2006-01-02 15:04:05"))
	}

	if len(djobs) == 0 {
		fmt.Fprintln(os.Stderr, "Error: no jobs with valid SCHEDULE found")
		os.Exit(1)
	}

	if once {
		for _, dj := range djobs {
			executeJob(dj.cfg, false)
		}
		return
	}

	// Signal handling for graceful shutdown.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)

	// Disk pressure alerts — buffered so the monitor never blocks.
	diskAlert := make(chan *Config, len(djobs))
	go monitorDisk(djobs, diskAlert, stop)

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			logInfo("Daemon received shutdown signal")
			return

		case cfg := <-diskAlert:
			logError("DISK CRITICAL on %s — triggering emergency rotation + cloud panic backup", cfg.LogDir)
			cfg.DateSuffix = time.Now().Format("20060102")
			cfg.BackupDate = cfg.DateSuffix
			executeJob(cfg, true) // emergency=true → triggers CLOUD_BACKUP_ON_PANIC if set
			// Reset that job's next-run after emergency rotation.
			for _, dj := range djobs {
				if dj.cfg == cfg {
					if nr, err := nextRunTime(cfg.Schedule, time.Now()); err == nil {
						dj.nextRun = nr
					}
				}
			}

		case now := <-ticker.C:
			for _, dj := range djobs {
				if now.Before(dj.nextRun) {
					continue
				}
				logInfo("Running scheduled job [%s]", dj.cfg.LogDir)
				dj.cfg.DateSuffix = now.Format("20060102")
				dj.cfg.BackupDate = dj.cfg.DateSuffix
				executeJob(dj.cfg, false)
				nr, err := nextRunTime(dj.cfg.Schedule, now)
				if err != nil {
					logError("Schedule error for job [%s]: %v", dj.cfg.JobName, err)
					continue
				}
				dj.nextRun = nr
				logInfo("Job [%s] next run: %s", dj.cfg.JobName, nr.Format("2006-01-02 15:04:05"))
			}
		}
	}
}

// executeJob runs a rotation job and optionally triggers cloud backup after.
// emergency=true means the job was triggered by disk pressure (panic mode).
func executeJob(cfg *Config, emergency bool) {
	excludePatterns := loadExcludePatterns(cfg.ExcludeFile)
	files := findLogFiles(cfg.LogDir, cfg.Pattern, excludePatterns)
	if len(files) == 0 {
		logInfo("Job [%s]: no files found in %s", cfg.JobName, cfg.LogDir)
		return
	}
	logInfo("Job [%s]: rotating %d file(s) in %s (emergency=%v)", cfg.JobName, len(files), cfg.LogDir, emergency)
	if cfg.Parallel {
		rotateParallel(files, cfg)
	} else {
		rotateSequential(files, cfg)
	}
	runCloudBackup(cfg, emergency)
}

func monitorDisk(jobs []*daemonJob, alert chan<- *Config, stop <-chan os.Signal) {
	if len(jobs) == 0 {
		return
	}
	interval := time.Duration(jobs[0].cfg.DiskCheckSec) * time.Second
	if interval < time.Second {
		interval = 60 * time.Second
	}
	lastAlert := make(map[*Config]time.Time)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			for _, dj := range jobs {
				cfg := dj.cfg
				_, freeMB, usedPct, err := diskStats(cfg.LogDir)
				if err != nil {
					logDebug("diskStats %s: %v", cfg.LogDir, err)
					continue
				}
				logDebug("Disk [%s]: %.1f%% used, %d MB free", cfg.JobName, usedPct, freeMB)
				if usedPct >= float64(cfg.DiskCriticalPct) {
					if time.Since(lastAlert[cfg]) < 5*time.Minute {
						continue
					}
					lastAlert[cfg] = time.Now()
					select {
					case alert <- cfg:
					default:
					}
				} else if freeMB < cfg.DiskMinFreeMB {
					logError("Disk low [%s]: %d MB free (min %d MB)", cfg.JobName, freeMB, cfg.DiskMinFreeMB)
				}
			}
		}
	}
}

func main() {
	cfg := parseFlags()

	// Daemon mode: load all job configs and run the scheduling loop.
	if cfg.Daemon || cfg.DaemonOnce {
		jobs := loadJobConfigs()
		if len(jobs) == 0 {
			fmt.Fprintln(os.Stderr, "Error: no jobs found in config (add SCHEDULE to global.conf or conf.d files)")
			os.Exit(1)
		}
		if err := initLogger(jobs[0].LogFile, jobs[0].LogLevel); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Could not initialize logging: %v\n", err)
		} else {
			defer closeLogger()
		}
		runDaemon(jobs, cfg.DaemonOnce)
		return
	}

	// Initialize logger (skip for special modes that output to stdout)
	if cfg.ReadFile == "" && !cfg.PassGen && !cfg.PassReset && len(os.Args) > 1 {
		if err := initLogger(cfg.LogFile, cfg.LogLevel); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Could not initialize logging: %v\n", err)
		} else {
			defer closeLogger()
			logInfo("global-logrotate v%s started", version)
			logDebug("Log level: %d, Log file: %s", cfg.LogLevel, cfg.LogFile)
		}
	}

	// Handle --pass-gen (generate new password)
	if cfg.PassGen {
		generatePassword()
		return
	}

	// Handle --pass-reset (reset password)
	if cfg.PassReset {
		resetPassword()
		return
	}

	// Handle --read mode
	if cfg.ReadFile != "" {
		if err := readLogFile(cfg.ReadFile, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if cfg.CustomPath {
		if info, err := os.Stat(cfg.LogDir); err != nil || !info.IsDir() {
			fmt.Fprintf(os.Stderr, "Error: Custom log path '%s' does not exist.\n", cfg.LogDir)
			logError("Custom log path '%s' does not exist", cfg.LogDir)
			os.Exit(1)
		}
	}

	// Validate encryption settings
	if cfg.Encrypt {
		if cfg.EncryptPassword == "" && cfg.EncryptPassHash == "" {
			fmt.Fprintln(os.Stderr, "Error: --encrypt requires password to be configured")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "First-time setup required! Run:")
			fmt.Fprintln(os.Stderr, "  global-logrotate --pass-gen")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "Or to reset existing password:")
			fmt.Fprintln(os.Stderr, "  global-logrotate --pass-reset")
			logError("Encryption requested but no password configured")
			os.Exit(1)
		}
	}

	logInfo("Starting rotation - Dir: %s, Pattern: %s, Encrypt: %v, DryRun: %v",
		cfg.LogDir, cfg.Pattern, cfg.Encrypt, cfg.DryRun)

	excludePatterns := loadExcludePatterns(cfg.ExcludeFile)
	logFiles := findLogFiles(cfg.LogDir, cfg.Pattern, excludePatterns)

	if len(logFiles) == 0 {
		fmt.Printf("No files matching pattern '%s' found in %s\n", cfg.Pattern, cfg.LogDir)
		logInfo("No files matching pattern '%s' found in %s", cfg.Pattern, cfg.LogDir)
		os.Exit(0)
	}

	logInfo("Found %d files to rotate", len(logFiles))
	logDebug("Files: %v", logFiles)

	if cfg.Parallel {
		logDebug("Using parallel rotation with %d jobs", cfg.ParallelJobs)
		rotateParallel(logFiles, cfg)
	} else {
		logDebug("Using sequential rotation")
		rotateSequential(logFiles, cfg)
	}

	logInfo("Rotation completed")
}

func generatePassword() {
	fmt.Println("=== Global Logrotate - Password Setup ===")
	fmt.Println()

	// Check if password already exists
	fileConfig := loadConfigFiles()
	if hash := getConfigDefault(fileConfig, "ENCRYPT_PASSWORD_HASH", ""); hash != "" {
		fmt.Println("A password is already configured.")
		fmt.Println()
		fmt.Println("To change the existing password, use:")
		fmt.Println("  global-logrotate --pass-reset")
		fmt.Println()
		return
	}

	fmt.Println("Choose password option:")
	fmt.Println("  1) Generate random password (recommended)")
	fmt.Println("  2) Enter custom password")
	fmt.Println()
	fmt.Print("Select [1/2]: ")

	var choice string
	fmt.Scanln(&choice)
	fmt.Println()

	var password string

	if choice == "2" {
		// Custom password
		var err error
		password, err = readPassword("Enter new password: ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
			os.Exit(1)
		}
		if len(password) < 8 {
			fmt.Fprintln(os.Stderr, "Error: Password must be at least 8 characters")
			os.Exit(1)
		}
		confirm, err := readPassword("Confirm password: ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
			os.Exit(1)
		}
		if password != confirm {
			fmt.Fprintln(os.Stderr, "Error: Passwords do not match")
			os.Exit(1)
		}
	} else {
		// Generate random password
		password = generateRandomPassword(24)
	}

	// Generate hash
	hash := sha256.Sum256([]byte(password))
	hashStr := hex.EncodeToString(hash[:])

	// Save hash to config
	if err := savePasswordHash(hashStr); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
		os.Exit(1)
	}

	// Save password to user's credentials file
	if err := savePasswordToCredentials(password); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Could not save to credentials file: %v\n", err)
	}

	// Mask password for display
	maskedPassword := maskPassword(password)

	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                    PASSWORD SETUP COMPLETE                       ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  Password: %-54s ║\n", maskedPassword)
	fmt.Println("╠══════════════════════════════════════════════════════════════════╣")
	fmt.Println("║  Password saved to credentials file. No need to enter it again. ║")
	fmt.Println("║  Keep your credentials file secure!                             ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("Password stored in:")
	fmt.Printf("  %s\n", getUserCredentialsFile())
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  # Rotate with encryption (password auto-loaded from credentials):")
	fmt.Println("  global-logrotate --encrypt -D -p /var/log/apps")
	fmt.Println()
	fmt.Println("  # Read encrypted logs:")
	fmt.Println("  global-logrotate --read /path/to/file.gz.enc")
	fmt.Println()
	fmt.Println("Config saved to: /etc/global-sys-utils/global.conf.d/encryption.conf")
}

// maskPassword masks a password showing only first and last character
func maskPassword(password string) string {
	if len(password) <= 2 {
		return "****"
	}
	masked := string(password[0]) + strings.Repeat("*", len(password)-2) + string(password[len(password)-1])
	return masked
}

// readPassword reads a password from terminal without echoing
func readPassword(prompt string) (string, error) {
	fmt.Print(prompt)

	// Check if stdin is a terminal
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		password, err := term.ReadPassword(fd)
		fmt.Println() // Print newline after hidden input
		if err != nil {
			return "", err
		}
		return string(password), nil
	}

	// Fallback for non-terminal (e.g., piped input)
	var password string
	fmt.Scanln(&password)
	return password, nil
}

// getUserCredentialsFile returns path to user's credentials file
func getUserCredentialsFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".global-sys-utils", "config", "credentials.ini")
}

// readPasswordFromCredentials reads password from user's credentials file
func readPasswordFromCredentials() string {
	credFile := getUserCredentialsFile()
	if credFile == "" {
		return ""
	}

	file, err := os.Open(credFile)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if idx := strings.Index(line, "="); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			value = strings.Trim(value, "\"'")
			if key == "LOGROTATE_PASSWORD" || key == "password" {
				return value
			}
		}
	}
	return ""
}

// savePasswordToCredentials saves password to user's credentials file
func savePasswordToCredentials(password string) error {
	credFile := getUserCredentialsFile()
	if credFile == "" {
		return fmt.Errorf("could not determine home directory")
	}

	// Create directory if needed
	credDir := filepath.Dir(credFile)
	if err := os.MkdirAll(credDir, 0700); err != nil {
		return err
	}

	content := fmt.Sprintf(`# Global Logrotate Credentials
# Generated: %s
# This file contains your encryption password
# Keep this file secure (chmod 600)

LOGROTATE_PASSWORD = %s
`, time.Now().Format("2006-01-02 15:04:05"), password)

	if err := os.WriteFile(credFile, []byte(content), 0600); err != nil {
		return err
	}

	return nil
}

func resetPassword() {
	fmt.Println("=== Global Logrotate - Password Reset ===")
	fmt.Println()

	// Check if password exists
	fileConfig := loadConfigFiles()
	existingHash := getConfigDefault(fileConfig, "ENCRYPT_PASSWORD_HASH", "")

	if existingHash == "" {
		fmt.Println("No existing password found. Use --pass-gen for initial setup.")
		return
	}

	// Verify current password
	currentPass, err := readPassword("Enter current password: ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
		os.Exit(1)
	}

	currentHash := sha256.Sum256([]byte(currentPass))
	currentHashStr := hex.EncodeToString(currentHash[:])

	if currentHashStr != existingHash {
		fmt.Fprintln(os.Stderr, "Error: Current password is incorrect")
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("Choose new password option:")
	fmt.Println("  1) Generate random password (recommended)")
	fmt.Println("  2) Enter custom password")
	fmt.Println()
	fmt.Print("Select [1/2]: ")

	var choice string
	fmt.Scanln(&choice)
	fmt.Println()

	var newPassword string

	if choice == "2" {
		var err error
		newPassword, err = readPassword("Enter new password: ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
			os.Exit(1)
		}
		if len(newPassword) < 8 {
			fmt.Fprintln(os.Stderr, "Error: Password must be at least 8 characters")
			os.Exit(1)
		}
		confirm, err := readPassword("Confirm new password: ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
			os.Exit(1)
		}
		if newPassword != confirm {
			fmt.Fprintln(os.Stderr, "Error: Passwords do not match")
			os.Exit(1)
		}
	} else {
		newPassword = generateRandomPassword(24)
	}

	// Generate new hash
	newHash := sha256.Sum256([]byte(newPassword))
	newHashStr := hex.EncodeToString(newHash[:])

	// Save hash to config
	if err := savePasswordHash(newHashStr); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
		os.Exit(1)
	}

	// Save password to user's credentials file
	if err := savePasswordToCredentials(newPassword); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Could not save to credentials file: %v\n", err)
	}

	// Mask password for display
	maskedPassword := maskPassword(newPassword)

	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                    PASSWORD RESET COMPLETE                       ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  New Password: %-50s ║\n", maskedPassword)
	fmt.Println("╠══════════════════════════════════════════════════════════════════╣")
	fmt.Println("║  WARNING: Previously encrypted files will still need the OLD    ║")
	fmt.Println("║  password to decrypt. Only new files will use this password.    ║")
	fmt.Println("║                                                                  ║")
	fmt.Println("║  Password saved to credentials file. No need to enter it again. ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("Password stored in:")
	fmt.Printf("  %s\n", getUserCredentialsFile())
}

func generateRandomPassword(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure means the OS entropy source is unavailable — no safe fallback exists.
		fmt.Fprintf(os.Stderr, "fatal: crypto/rand unavailable: %v\n", err)
		os.Exit(1)
	}
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b)
}

func savePasswordHash(hashStr string) error {
	// Ensure directory exists
	if err := os.MkdirAll(configDropinDir, 0755); err != nil {
		return err
	}

	configPath := filepath.Join(configDropinDir, "encryption.conf")
	content := fmt.Sprintf(`# Global Logrotate Encryption Configuration
# Generated: %s
# DO NOT share this file or commit to version control

# Enable encryption by default (optional)
# ENCRYPT = true

# SHA-256 hash of encryption password
ENCRYPT_PASSWORD_HASH = %s
`, time.Now().Format("2006-01-02 15:04:05"), hashStr)

	return os.WriteFile(configPath, []byte(content), 0600)
}

func loadConfigFiles() map[string]string {
	config := make(map[string]string)

	// Load main config
	loadConfigFile(mainConfigFile, config)

	// Load drop-in configs (sorted for predictable order)
	if files, err := filepath.Glob(filepath.Join(configDropinDir, "*.conf")); err == nil {
		sort.Strings(files)
		for _, f := range files {
			loadConfigFile(f, config)
		}
	}

	return config
}

func loadConfigFile(path string, config map[string]string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if idx := strings.Index(line, "="); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			value = strings.Trim(value, "\"'")
			config[key] = value
		}
	}
}

func parseFlags() *Config {
	fileConfig := loadConfigFiles()
	cfg := buildConfig(fileConfig)

	var useFullTime, useDateOnly, showVersion, showHelp, enableEncrypt bool
	var readFile string
	var passGen, passReset bool
	var logLevel string

	flag.BoolVar(&useFullTime, "H", false, "Use full timestamp format (YYYYMMDDTHH:MM:SS)")
	flag.BoolVar(&useDateOnly, "D", false, "Use date-only format (YYYYMMDD)")
	flag.StringVar(&cfg.Pattern, "pattern", cfg.Pattern, "File pattern to rotate")
	flag.StringVar(&cfg.LogDir, "p", cfg.LogDir, "Specify custom log directory")
	flag.BoolVar(&cfg.DryRun, "n", cfg.DryRun, "Dry-run mode (no changes made)")
	flag.StringVar(&cfg.OldLogsDir, "o", cfg.OldLogsDir, "Specify old_logs directory")
	flag.StringVar(&cfg.ExcludeFile, "exclude-from", cfg.ExcludeFile, "Path to file containing exclude patterns")
	flag.IntVar(&cfg.ParallelJobs, "parallel", cfg.ParallelJobs, "Rotate up to N log files in parallel")
	flag.BoolVar(&enableEncrypt, "encrypt", cfg.Encrypt, "Encrypt rotated logs with AES-256-GCM")
	flag.StringVar(&readFile, "read", "", "Read a rotated log file (.gz or .gz.enc)")
	flag.BoolVar(&passGen, "pass-gen", false, "Generate and configure encryption password (first-time setup)")
	flag.BoolVar(&passReset, "pass-reset", false, "Reset/change encryption password")
	flag.StringVar(&cfg.LogFile, "log-file", cfg.LogFile, "Path to log file")
	flag.StringVar(&logLevel, "log-level", "", "Log level: error, info, debug")
	flag.BoolVar(&cfg.Daemon, "daemon", false, "Run as daemon; reads SCHEDULE from config files")
	flag.BoolVar(&cfg.DaemonOnce, "daemon-once", false, "Run all scheduled jobs once then exit (for systemd timers)")
	flag.BoolVar(&showVersion, "version", false, "Show version")
	flag.BoolVar(&showHelp, "h", false, "Show help")

	flag.Usage = showUsage
	flag.Parse()

	if showVersion {
		fmt.Printf("global-logrotate version %s\n", version)
		os.Exit(0)
	}

	if showHelp {
		showUsage()
		os.Exit(0)
	}

	cfg.ReadFile = readFile
	cfg.PassGen = passGen
	cfg.PassReset = passReset

	cfg.ReadFile = readFile
	cfg.PassGen = passGen
	cfg.PassReset = passReset

	if enableEncrypt {
		cfg.Encrypt = true
	}
	if logLevel != "" {
		cfg.LogLevel = parseLogLevel(logLevel)
	}

	// Daemon flags bypass the rest of the normal single-run validation.
	if cfg.Daemon || cfg.DaemonOnce {
		return cfg
	}

	if cfg.ReadFile != "" || cfg.PassGen || cfg.PassReset {
		return cfg
	}

	if len(os.Args) == 1 {
		showUsage()
		os.Exit(0)
	}

	cfg.CustomPath = cfg.LogDir != defaultDir

	if useFullTime {
		cfg.DateSuffix = time.Now().Format("20060102T15:04:05")
	} else if useDateOnly {
		cfg.DateSuffix = time.Now().Format("20060102")
	} else if cfg.DateFormat == "full" {
		cfg.DateSuffix = time.Now().Format("20060102T15:04:05")
	} else {
		cfg.DateSuffix = time.Now().Format("20060102")
	}

	if cfg.ParallelJobs <= 0 {
		fmt.Fprintln(os.Stderr, "Error: --parallel must be >= 1")
		os.Exit(1)
	}

	cfg.Parallel = cfg.ParallelJobs > 1
	cfg.LogDir = strings.TrimSuffix(cfg.LogDir, "/")
	cfg.BackupDate = time.Now().Format("20060102")

	return cfg
}

func getConfigDefault(config map[string]string, key, defaultVal string) string {
	if val, ok := config[key]; ok && val != "" {
		return val
	}
	return defaultVal
}

func getConfigDefaultInt(config map[string]string, key string, defaultVal int) int {
	if val, ok := config[key]; ok && val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

func getConfigDefaultBool(config map[string]string, key string, defaultVal bool) bool {
	if val, ok := config[key]; ok {
		lower := strings.ToLower(val)
		return lower == "true" || lower == "yes" || lower == "1"
	}
	return defaultVal
}

func showUsage() {
	fmt.Println("Usage: global-logrotate [OPTIONS]")
	fmt.Println()
	fmt.Println("A fast log rotation utility written in Go (zero external dependencies)")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -H                  Use full timestamp format (YYYYMMDDTHH:MM:SS)")
	fmt.Println("  -D                  Use date-only format (YYYYMMDD)")
	fmt.Println("  --pattern <glob>    File pattern to rotate (default: *.log)")
	fmt.Println("  -p <path>           Specify custom log directory (default: /var/log/apps)")
	fmt.Println("  -n                  Dry-run mode (no changes made)")
	fmt.Println("  --exclude-from      Path to file containing exclude patterns")
	fmt.Println("  -o <path>           Specify old_logs directory (default: <logdir>/old_logs)")
	fmt.Println("  --parallel N        Rotate up to N log files in parallel (default: 4)")
	fmt.Println("  --encrypt           Encrypt rotated logs with AES-256-GCM")
	fmt.Println("  --read <file>       Read a rotated log file (.gz or .gz.enc)")
	fmt.Println("  --pass-gen          Generate and setup encryption password (REQUIRED for first use)")
	fmt.Println("  --pass-reset        Reset/change encryption password")
	fmt.Println("  --log-file <path>   Path to log file (default: /var/log/global-sys-utils/global-logrotate.log)")
	fmt.Println("  --log-level <level> Log level: error, info, debug (default: info)")
	fmt.Println("  --version           Show version")
	fmt.Println("  -h                  Show this help")
	fmt.Println()
	fmt.Println("Log Levels:")
	fmt.Println("  error (0)  - Only errors")
	fmt.Println("  info  (1)  - Errors and general information (default)")
	fmt.Println("  debug (2)  - All messages including debug details")
	fmt.Println()
	fmt.Println("First-Time Encryption Setup:")
	fmt.Println("  global-logrotate --pass-gen     # Generate password (required before using --encrypt)")
	fmt.Println()
	fmt.Println("Password Management:")
	fmt.Println("  global-logrotate --pass-reset   # Change existing password")
	fmt.Println()
	fmt.Println("Configuration files:")
	fmt.Println("  /etc/global-sys-utils/global.conf")
	fmt.Println("  /etc/global-sys-utils/global.conf.d/*.conf")
	fmt.Println()
	fmt.Println("Logging Configuration (in config file):")
	fmt.Println("  LOG_FILE  = /var/log/global-sys-utils/global-logrotate.log")
	fmt.Println("  LOG_LEVEL = info  # error, info, or debug")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  global-logrotate -D -p /var/log/myapp                    # Basic rotation")
	fmt.Println("  global-logrotate --pass-gen                              # Setup encryption")
	fmt.Println("  global-logrotate --encrypt -D -p /var/log/secure         # Rotate with encryption")
	fmt.Println("  global-logrotate --read /path/to/file.gz.enc             # Read encrypted log")
	fmt.Println("  global-logrotate -D -p /var/log/apps --log-level debug   # With debug logging")
}

func loadExcludePatterns(excludeFile string) []string {
	if excludeFile == "" {
		return nil
	}

	file, err := os.Open(excludeFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Exclude file '%s' does not exist.\n", excludeFile)
		logError("Exclude file '%s' does not exist", excludeFile)
		os.Exit(1)
	}
	defer file.Close()

	fmt.Printf("Excluding patterns from: %s\n", excludeFile)
	logInfo("Loading exclude patterns from: %s", excludeFile)
	var patterns []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			fmt.Printf("  - %s\n", line)
			logDebug("Exclude pattern: %s", line)
			patterns = append(patterns, line)
		}
	}
	return patterns
}

func findLogFiles(logDir, pattern string, excludePatterns []string) []fileInfo {
	var files []fileInfo

	logDebug("Searching for files in %s with pattern %s", logDir, pattern)

	err := filepath.WalkDir(logDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			logInfo("Skipping inaccessible path %s: %v", path, err)
			return nil
		}
		if d.IsDir() {
			return nil
		}

		matched, err := filepath.Match(pattern, d.Name())
		if err != nil || !matched {
			return nil
		}

		for _, excludePattern := range excludePatterns {
			if matchExclude, _ := filepath.Match(excludePattern, path); matchExclude {
				logDebug("Excluding file (path match): %s", path)
				return nil
			}
			if matchExclude, _ := filepath.Match(excludePattern, d.Name()); matchExclude {
				logDebug("Excluding file (name match): %s", path)
				return nil
			}
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		logDebug("Found file: %s (size: %d)", path, info.Size())
		files = append(files, fileInfo{path: path, size: info.Size()})
		return nil
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error walking directory: %v\n", err)
		logError("Error walking directory %s: %v", logDir, err)
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].size < files[j].size
	})

	return files
}

type fileInfo struct {
	path string
	size int64
}

func rotateSequential(files []fileInfo, cfg *Config) {
	for _, f := range files {
		rotateLogFile(f.path, cfg)
	}
}

func rotateParallel(files []fileInfo, cfg *Config) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, cfg.ParallelJobs)

	for _, f := range files {
		wg.Add(1)
		sem <- struct{}{}
		go func(path string) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					fmt.Fprintf(os.Stderr, "panic processing %s: %v\n", path, r)
					logError("panic processing %s: %v", path, r)
				}
			}()
			rotateLogFile(path, cfg)
		}(f.path)
	}
	wg.Wait()
}

func rotateLogFile(logFile string, cfg *Config) {
	logDebug("Processing file: %s", logFile)

	info, err := os.Stat(logFile)
	if err != nil {
		fmt.Printf("%s: Skipping missing file: %s\n", timestamp(), logFile)
		logError("Skipping missing file: %s", logFile)
		return
	}
	if info.Size() == 0 {
		fmt.Printf("%s: Skipping empty file: %s\n", timestamp(), logFile)
		logDebug("Skipping empty file: %s", logFile)
		return
	}

	originalSize := info.Size()

	// Get file ownership and permissions
	stat := info.Sys().(*syscall.Stat_t)
	uid := int(stat.Uid)
	gid := int(stat.Gid)
	mode := info.Mode()

	logDir := filepath.Dir(logFile)
	logName := filepath.Base(logFile)
	rotatedBasename := fmt.Sprintf("%s.%s", logName, cfg.DateSuffix)

	var backupRoot string
	if cfg.OldLogsDir != "" {
		backupRoot = cfg.OldLogsDir
	} else {
		backupRoot = filepath.Join(logDir, "old_logs")
	}

	backupDir := filepath.Join(backupRoot, cfg.BackupDate)

	// Determine final file extension
	var archivedFile string
	if cfg.Encrypt {
		archivedFile = filepath.Join(backupDir, rotatedBasename+".gz.enc")
	} else {
		archivedFile = filepath.Join(backupDir, rotatedBasename+".gz")
	}

	if _, err := os.Stat(archivedFile); err == nil {
		fmt.Printf("%s: Already rotated, skipping: %s\n", timestamp(), logFile)
		logInfo("Already rotated, skipping: %s", logFile)
		return
	}

	if cfg.DryRun {
		encStatus := ""
		if cfg.Encrypt {
			encStatus = " [ENCRYPTED]"
		}
		fmt.Printf("[DRY-RUN] Would Rotate: %s (%s) -> %s%s\n", logFile, formatSize(originalSize), archivedFile, encStatus)
		logInfo("[DRY-RUN] Would rotate: %s -> %s", logFile, archivedFile)
		return
	}

	// Create backup directory
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating backup dir: %v\n", err)
		logError("Error creating backup dir %s: %v", backupDir, err)
		return
	}

	// Stream the file through gzip — avoids holding both original and compressed bytes in memory.
	f, err := os.Open(logFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		logError("Error reading file %s: %v", logFile, err)
		return
	}
	compressedData, err := compressGzip(f)
	f.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error compressing file: %v\n", err)
		logError("Error compressing file %s: %v", logFile, err)
		return
	}

	logDebug("Compressed to %d bytes", len(compressedData))

	// Encrypt if enabled
	var finalData []byte
	if cfg.Encrypt {
		password := getEncryptionPassword(cfg)
		if password == "" {
			fmt.Fprintf(os.Stderr, "Error: No encryption password configured\n")
			logError("No encryption password configured for %s", logFile)
			return
		}

		finalData, err = encryptData(compressedData, password)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error encrypting file: %v\n", err)
			logError("Error encrypting file %s: %v", logFile, err)
			return
		}
		logDebug("Encrypted to %d bytes", len(finalData))
	} else {
		finalData = compressedData
	}

	// Strip setuid/setgid/execute bits from the archive — a compressed log file
	// has no business being executable, and inheriting setuid from the source
	// would be a privilege-escalation risk.
	archiveMode := mode &^ (os.ModeSetuid | os.ModeSetgid) & 0666

	// Disk space guard: ensure the backup directory has enough room for this archive.
	// If the disk is too full to write even the compressed bytes, skip this file
	// rather than filling the disk entirely and crashing the host.
	if cfg.DiskMinFreeMB > 0 {
		if _, freeMB, _, diskErr := diskStats(backupDir); diskErr == nil {
			needMB := int64(len(finalData))/(1024*1024) + 1
			if freeMB-needMB < cfg.DiskMinFreeMB {
				fmt.Fprintf(os.Stderr, "SKIP (disk full): %s — only %d MB free, need %d MB buffer\n",
					logFile, freeMB, cfg.DiskMinFreeMB)
				logError("Skipping archive for %s: %d MB free < %d MB minimum", logFile, freeMB, cfg.DiskMinFreeMB)
				return
			}
		}
	}

	// Write to a temp file first. os.Rename is atomic on the same filesystem,
	// so a crash between write and rename leaves the original file intact.
	tmpFile := archivedFile + ".tmp"
	if err := os.WriteFile(tmpFile, finalData, archiveMode); err != nil {
		os.Remove(tmpFile) // clean up partial write
		fmt.Fprintf(os.Stderr, "Error writing archive: %v\n", err)
		logError("Error writing archive %s: %v", tmpFile, err)
		return
	}

	if err := os.Rename(tmpFile, archivedFile); err != nil {
		os.Remove(tmpFile)
		fmt.Fprintf(os.Stderr, "Error finalizing archive: %v\n", err)
		logError("Error finalizing archive %s: %v", archivedFile, err)
		return
	}

	// Truncate original only after archive is safely on disk.
	if err := os.Truncate(logFile, 0); err != nil {
		fmt.Fprintf(os.Stderr, "Error truncating file: %v\n", err)
		logError("Error truncating file %s: %v", logFile, err)
		return
	}

	// Restore ownership and permissions; non-fatal but surfaced at INFO so
	// operators running as non-root notice the degraded ownership.
	if err := os.Chown(archivedFile, uid, gid); err != nil {
		logInfo("Could not restore ownership on %s: %v", archivedFile, err)
	}
	if err := os.Chmod(archivedFile, archiveMode); err != nil {
		logInfo("Could not restore permissions on %s: %v", archivedFile, err)
	}

	// Get compressed/encrypted file size and calculate compression stats
	compressedSize := int64(len(finalData))

	compressionRatio := float64(0)
	if originalSize > 0 {
		compressionRatio = max((1-float64(compressedSize)/float64(originalSize))*100, 0)
	}

	saved := max(originalSize-compressedSize, 0)

	encStatus := ""
	if cfg.Encrypt {
		encStatus = " [ENCRYPTED]"
	}

	fmt.Printf("%s: Rotated: %s -> %s%s\n", timestamp(), logFile, archivedFile, encStatus)
	fmt.Printf("           Size: %s -> %s (%.1f%% compression, saved %s)\n",
		formatSize(originalSize), formatSize(compressedSize), compressionRatio, formatSize(saved))

	logInfo("Rotated: %s -> %s (size: %d -> %d, ratio: %.1f%%)",
		logFile, archivedFile, originalSize, compressedSize, compressionRatio)
}

// compressGzip reads from r and returns gzip-compressed bytes.
// Uses io.Reader so callers can stream directly from a file without loading the full content.
func compressGzip(r io.Reader) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := io.Copy(w, r); err != nil {
		w.Close()
		return nil, fmt.Errorf("compressing: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("finalizing gzip stream: %w", err)
	}
	return buf.Bytes(), nil
}

// decompressGzip decompresses gzip-compressed bytes.
func decompressGzip(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("creating gzip reader: %w", err)
	}
	defer r.Close()
	return io.ReadAll(r)
}

// deriveKey derives an AES-256 key from password using PBKDF2
func deriveKey(password string, salt []byte) []byte {
	return pbkdf2.Key([]byte(password), salt, iterations, keySize, sha256.New)
}

// encryptData encrypts plaintext with AES-256-GCM using a PBKDF2-derived key.
// Output format: MAGIC(4) + SALT(32) + NONCE(12) + CIPHERTEXT+TAG
func encryptData(plaintext []byte, password string) ([]byte, error) {
	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generating salt: %w", err)
	}

	key := deriveKey(password, salt)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("creating cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creating GCM: %w", err)
	}

	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	result := make([]byte, 0, len(encryptMagic)+saltSize+nonceSize+len(ciphertext))
	result = append(result, encryptMagic...)
	result = append(result, salt...)
	result = append(result, nonce...)
	result = append(result, ciphertext...)

	return result, nil
}

// decryptData decrypts AES-256-GCM data produced by encryptData.
// Format: MAGIC(4) + SALT(32) + NONCE(12) + CIPHERTEXT+TAG
func decryptData(data []byte, password string) ([]byte, error) {
	minLen := len(encryptMagic) + saltSize + nonceSize + 16 // 16 = GCM tag
	if len(data) < minLen {
		return nil, fmt.Errorf("encrypted data too short (%d bytes)", len(data))
	}

	if !bytes.Equal(data[:len(encryptMagic)], encryptMagic) {
		return nil, fmt.Errorf("invalid encrypted file format: bad magic bytes")
	}

	offset := len(encryptMagic)
	salt := data[offset : offset+saltSize]
	offset += saltSize
	nonce := data[offset : offset+nonceSize]
	offset += nonceSize
	ciphertext := data[offset:]

	key := deriveKey(password, salt)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("creating cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creating GCM: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed (wrong password or corrupted file): %w", err)
	}

	return plaintext, nil
}

func matchesHash(password, wantHex string) bool {
	h := sha256.Sum256([]byte(password))
	return hex.EncodeToString(h[:]) == wantHex
}

func getEncryptionPassword(cfg *Config) string {
	passwordMu.Lock()
	defer passwordMu.Unlock()

	if cachedPassword != "" {
		return cachedPassword
	}

	if cfg.EncryptPassword != "" {
		cachedPassword = cfg.EncryptPassword
		return cachedPassword
	}

	credPass := readPasswordFromCredentials()
	if credPass != "" {
		if cfg.EncryptPassHash != "" {
			if matchesHash(credPass, cfg.EncryptPassHash) {
				cachedPassword = credPass
				logDebug("Password loaded from credentials file")
				return cachedPassword
			}
			logDebug("Password from credentials file does not match hash")
		} else {
			// No hash configured — cannot verify correctness, so don't cache.
			// Re-reading the credentials file per file is cheap and avoids
			// propagating a wrong password silently across all files.
			logDebug("Password loaded from credentials file (no hash verification)")
			return credPass
		}
	}

	envPass := os.Getenv("LOGROTATE_PASSWORD")
	if envPass != "" {
		if cfg.EncryptPassHash != "" {
			if matchesHash(envPass, cfg.EncryptPassHash) {
				cachedPassword = envPass
				logDebug("Password loaded from environment variable")
				return cachedPassword
			}
			fmt.Fprintf(os.Stderr, "Warning: LOGROTATE_PASSWORD does not match configured hash\n")
			logError("LOGROTATE_PASSWORD environment variable does not match configured hash")
		} else {
			// No hash — don't cache, same reasoning as credentials file path.
			logDebug("Password loaded from environment variable (no hash verification)")
			return envPass
		}
	}

	if cfg.EncryptPassHash != "" {
		password, err := readPassword("Enter encryption password: ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
			return ""
		}
		if matchesHash(password, cfg.EncryptPassHash) {
			cachedPassword = password
			return cachedPassword
		}
		fmt.Fprintf(os.Stderr, "Error: Password does not match configured hash\n")
		logError("Entered password does not match configured hash")
		return ""
	}

	return ""
}

func readLogFile(filePath string, cfg *Config) error {
	if _, err := os.Stat(filePath); err != nil {
		return fmt.Errorf("file not found: %s", filePath)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	var content []byte

	if strings.HasSuffix(filePath, ".gz.enc") {
		// Encrypted and compressed file (new format)
		content, err = readEncryptedGzFile(data, cfg)
	} else if strings.HasSuffix(filePath, ".enc") {
		// Encrypted only
		content, err = readEncryptedFile(data, cfg)
	} else if strings.HasSuffix(filePath, ".gz.gpg") {
		// Legacy GPG encrypted file
		return fmt.Errorf("legacy GPG format (.gz.gpg) is no longer supported. Please use gpg command directly to decrypt")
	} else if strings.HasSuffix(filePath, ".gz") {
		// Compressed only
		content, err = decompressGzip(data)
	} else {
		// Plain text
		content = data
	}

	if err != nil {
		return err
	}

	fmt.Print(string(content))
	return nil
}

func readEncryptedFile(data []byte, cfg *Config) ([]byte, error) {
	password := getDecryptionPassword(cfg)
	if password == "" {
		return nil, fmt.Errorf("no password provided for decryption")
	}

	return decryptData(data, password)
}

func readEncryptedGzFile(data []byte, cfg *Config) ([]byte, error) {
	password := getDecryptionPassword(cfg)
	if password == "" {
		return nil, fmt.Errorf("no password provided for decryption")
	}

	// Decrypt first
	compressed, err := decryptData(data, password)
	if err != nil {
		return nil, err
	}

	// Then decompress
	return decompressGzip(compressed)
}

func getDecryptionPassword(cfg *Config) string {
	if cfg.EncryptPassword != "" {
		return cfg.EncryptPassword
	}

	credPass := readPasswordFromCredentials()
	if credPass != "" {
		if cfg.EncryptPassHash != "" {
			if matchesHash(credPass, cfg.EncryptPassHash) {
				return credPass
			}
		} else {
			return credPass
		}
	}

	envPass := os.Getenv("LOGROTATE_PASSWORD")
	if envPass != "" {
		if cfg.EncryptPassHash != "" {
			if matchesHash(envPass, cfg.EncryptPassHash) {
				return envPass
			}
			fmt.Fprintf(os.Stderr, "Warning: LOGROTATE_PASSWORD does not match configured hash\n")
		} else {
			return envPass
		}
	}

	password, err := readPassword("Enter decryption password: ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
		return ""
	}

	if cfg.EncryptPassHash != "" && !matchesHash(password, cfg.EncryptPassHash) {
		fmt.Fprintf(os.Stderr, "Error: Password does not match configured hash\n")
		return ""
	}

	return password
}

func formatSize(bytes int64) string {
	const (
		B  = 1
		KB = 1024 * B
		MB = 1024 * KB
		GB = 1024 * MB
		TB = 1024 * GB
	)

	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.2f TB", float64(bytes)/float64(TB))
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func timestamp() string {
	return time.Now().Format("Mon Jan 2 15:04:05 MST 2006")
}
