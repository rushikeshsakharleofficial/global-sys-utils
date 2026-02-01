package main

import (
	"bufio"
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
)

// Log levels
const (
	LogLevelError = iota
	LogLevelInfo
	LogLevelDebug
)

// Magic bytes to identify our encrypted format
var encryptMagic = []byte("GLRE") // Global LogRotate Encrypted

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
	// Logging config
	LogFile  string
	LogLevel int
}

// initLogger initializes the global logger
func initLogger(logFile string, level int) error {
	// Create log directory if needed
	logDir := filepath.Dir(logFile)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %v", err)
	}

	// Open log file (append mode)
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %v", err)
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

// logWrite writes a log entry
func logWrite(level int, format string, args ...interface{}) {
	if logger == nil || level > logger.level {
		return
	}

	logger.mu.Lock()
	defer logger.mu.Unlock()

	levelStr := "INFO"
	switch level {
	case LogLevelError:
		levelStr = "ERROR"
	case LogLevelInfo:
		levelStr = "INFO"
	case LogLevelDebug:
		levelStr = "DEBUG"
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	message := fmt.Sprintf(format, args...)
	logLine := fmt.Sprintf("[%s] [%s] %s\n", timestamp, levelStr, message)

	logger.file.WriteString(logLine)
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

func main() {
	cfg := parseFlags()

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
		// Fallback to time-based if crypto/rand fails
		b = []byte(fmt.Sprintf("%d", time.Now().UnixNano()))
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

	cfg := &Config{
		LogDir:          getConfigDefault(fileConfig, "LOG_DIR", defaultDir),
		Pattern:         getConfigDefault(fileConfig, "PATTERN", "*.log"),
		ParallelJobs:    getConfigDefaultInt(fileConfig, "PARALLEL_JOBS", defaultJobs),
		OldLogsDir:      getConfigDefault(fileConfig, "OLD_LOGS_DIR", ""),
		ExcludeFile:     getConfigDefault(fileConfig, "EXCLUDE_FILE", ""),
		DateFormat:      getConfigDefault(fileConfig, "DATE_FORMAT", "date"),
		DryRun:          getConfigDefaultBool(fileConfig, "DRY_RUN", false),
		Encrypt:         getConfigDefaultBool(fileConfig, "ENCRYPT", false),
		EncryptPassword: getConfigDefault(fileConfig, "ENCRYPT_PASSWORD", ""),
		EncryptPassHash: getConfigDefault(fileConfig, "ENCRYPT_PASSWORD_HASH", ""),
		// Logging config
		LogFile:  getConfigDefault(fileConfig, "LOG_FILE", defaultLogFile),
		LogLevel: parseLogLevel(getConfigDefault(fileConfig, "LOG_LEVEL", "info")),
	}

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

	if enableEncrypt {
		cfg.Encrypt = true
	}

	// Override log level from command line
	if logLevel != "" {
		cfg.LogLevel = parseLogLevel(logLevel)
	}

	// If special modes, return early
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

	cfg.Parallel = cfg.ParallelJobs > 1
	cfg.LogDir = strings.TrimSuffix(cfg.LogDir, "/")

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
			logDebug("Error accessing path %s: %v", path, err)
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

	backupDir := filepath.Join(backupRoot, time.Now().Format("20060102"))

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

	// Read original file
	data, err := os.ReadFile(logFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		logError("Error reading file %s: %v", logFile, err)
		return
	}

	logDebug("Read %d bytes from %s", len(data), logFile)

	// Compress with gzip
	compressedData, err := compressGzip(data)
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

	// Write archived file
	if err := os.WriteFile(archivedFile, finalData, mode); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing archived file: %v\n", err)
		logError("Error writing archived file %s: %v", archivedFile, err)
		return
	}

	// Truncate original file
	if err := os.Truncate(logFile, 0); err != nil {
		fmt.Fprintf(os.Stderr, "Error truncating file: %v\n", err)
		logError("Error truncating file %s: %v", logFile, err)
		return
	}

	// Restore ownership
	if err := os.Chown(archivedFile, uid, gid); err != nil {
		logDebug("Could not restore ownership on %s: %v", archivedFile, err)
	}

	// Restore permissions
	if err := os.Chmod(archivedFile, mode); err != nil {
		logDebug("Could not restore permissions on %s: %v", archivedFile, err)
	}

	// Get compressed/encrypted file size and calculate compression stats
	compressedSize := int64(len(finalData))

	compressionRatio := float64(0)
	if originalSize > 0 {
		compressionRatio = (1 - float64(compressedSize)/float64(originalSize)) * 100
	}

	saved := originalSize - compressedSize

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

// compressGzip compresses data using gzip
func compressGzip(data []byte) ([]byte, error) {
	var buf strings.Builder
	w := gzip.NewWriter(&buf)
	_, err := w.Write(data)
	if err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

// decompressGzip decompresses gzip data
func decompressGzip(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// deriveKey derives an AES-256 key from password using PBKDF2
func deriveKey(password string, salt []byte) []byte {
	return pbkdf2.Key([]byte(password), salt, iterations, keySize, sha256.New)
}

// encryptData encrypts data using AES-256-GCM
// Format: MAGIC(4) + SALT(32) + NONCE(12) + CIPHERTEXT
func encryptData(plaintext []byte, password string) ([]byte, error) {
	// Generate random salt
	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}

	// Derive key from password
	key := deriveKey(password, salt)

	// Create cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	// Generate random nonce
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	// Encrypt
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	// Combine: MAGIC + SALT + NONCE + CIPHERTEXT
	result := make([]byte, 0, len(encryptMagic)+saltSize+nonceSize+len(ciphertext))
	result = append(result, encryptMagic...)
	result = append(result, salt...)
	result = append(result, nonce...)
	result = append(result, ciphertext...)

	return result, nil
}

// decryptData decrypts data using AES-256-GCM
func decryptData(data []byte, password string) ([]byte, error) {
	minLen := len(encryptMagic) + saltSize + nonceSize + 16 // 16 = min GCM tag
	if len(data) < minLen {
		return nil, fmt.Errorf("encrypted data too short")
	}

	// Check magic
	if string(data[:len(encryptMagic)]) != string(encryptMagic) {
		return nil, fmt.Errorf("invalid encrypted file format")
	}

	offset := len(encryptMagic)

	// Extract salt
	salt := data[offset : offset+saltSize]
	offset += saltSize

	// Extract nonce
	nonce := data[offset : offset+nonceSize]
	offset += nonceSize

	// Extract ciphertext
	ciphertext := data[offset:]

	// Derive key from password
	key := deriveKey(password, salt)

	// Create cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	// Decrypt
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed - incorrect password or corrupted file")
	}

	return plaintext, nil
}

func getEncryptionPassword(cfg *Config) string {
	passwordMu.Lock()
	defer passwordMu.Unlock()

	// Return cached password if available
	if cachedPassword != "" {
		return cachedPassword
	}

	// If plain password is set in config (not recommended but supported)
	if cfg.EncryptPassword != "" {
		cachedPassword = cfg.EncryptPassword
		return cachedPassword
	}

	// Check user's credentials file first (~/.global-sys-utils/config/credentials.ini)
	credPass := readPasswordFromCredentials()
	if credPass != "" {
		// If hash is configured, verify it
		if cfg.EncryptPassHash != "" {
			hash := sha256.Sum256([]byte(credPass))
			hashStr := hex.EncodeToString(hash[:])
			if hashStr == cfg.EncryptPassHash {
				cachedPassword = credPass
				logDebug("Password loaded from credentials file")
				return cachedPassword
			}
			logDebug("Password from credentials file does not match hash")
		} else {
			cachedPassword = credPass
			logDebug("Password loaded from credentials file (no hash verification)")
			return cachedPassword
		}
	}

	// Check environment variable
	envPass := os.Getenv("LOGROTATE_PASSWORD")
	if envPass != "" {
		// If hash is configured, verify it
		if cfg.EncryptPassHash != "" {
			hash := sha256.Sum256([]byte(envPass))
			hashStr := hex.EncodeToString(hash[:])
			if hashStr == cfg.EncryptPassHash {
				cachedPassword = envPass
				logDebug("Password loaded from environment variable")
				return cachedPassword
			}
			fmt.Fprintf(os.Stderr, "Warning: LOGROTATE_PASSWORD does not match configured hash\n")
			logError("LOGROTATE_PASSWORD environment variable does not match configured hash")
		} else {
			cachedPassword = envPass
			logDebug("Password loaded from environment variable (no hash verification)")
			return cachedPassword
		}
	}

	// If hash is set, prompt for password (only once)
	if cfg.EncryptPassHash != "" {
		password, err := readPassword("Enter encryption password: ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
			return ""
		}

		// Verify
		hash := sha256.Sum256([]byte(password))
		hashStr := hex.EncodeToString(hash[:])
		if hashStr == cfg.EncryptPassHash {
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
	// If plain password is set
	if cfg.EncryptPassword != "" {
		return cfg.EncryptPassword
	}

	// Check user's credentials file first (~/.global-sys-utils/config/credentials.ini)
	credPass := readPasswordFromCredentials()
	if credPass != "" {
		// If hash is configured, verify it
		if cfg.EncryptPassHash != "" {
			hash := sha256.Sum256([]byte(credPass))
			hashStr := hex.EncodeToString(hash[:])
			if hashStr == cfg.EncryptPassHash {
				return credPass
			}
			// Password doesn't match hash, continue to other methods
		} else {
			return credPass
		}
	}

	// Check environment variable
	envPass := os.Getenv("LOGROTATE_PASSWORD")
	if envPass != "" {
		// If hash is configured, verify it
		if cfg.EncryptPassHash != "" {
			hash := sha256.Sum256([]byte(envPass))
			hashStr := hex.EncodeToString(hash[:])
			if hashStr == cfg.EncryptPassHash {
				return envPass
			}
			fmt.Fprintf(os.Stderr, "Warning: LOGROTATE_PASSWORD does not match configured hash\n")
		} else {
			return envPass
		}
	}

	// Prompt for password (only for users without credentials.ini or env var)
	password, err := readPassword("Enter decryption password: ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
		return ""
	}

	// If hash is configured, verify it
	if cfg.EncryptPassHash != "" {
		hash := sha256.Sum256([]byte(password))
		hashStr := hex.EncodeToString(hash[:])
		if hashStr != cfg.EncryptPassHash {
			fmt.Fprintf(os.Stderr, "Error: Password does not match configured hash\n")
			return ""
		}
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
