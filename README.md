# 🛠️ global-sys-utils

A collection of scripts for log rotation, backup, and restore operations supporting AWS S3 and Google Cloud Storage, with parallel and pattern filtering support.

## ✨ Features

- 📜 **Log Rotation**: Automated log file rotation and compression
- ☁️ **Cloud Backup**: Support for AWS S3 and Google Cloud Storage
- 🔄 **Restore Operations**: Easy restoration of backed up files
- 🔍 **Pattern Filtering**: Selective backup/restore using pattern matching
- ⚡ **Parallel Processing**: Improved performance through parallel operations
- 🚫 **Exclusion Support**: Skip specified log files or patterns from rotation

## 📥 Installation

The package is available as an RPM for RHEL/CentOS 9:

```bash
git clone https://github.com/rushikeshsakharleofficial/global-sys-utils.git
cd global-sys-utils
rpm -ivh RPMS/noarch/global-sys-utils-1.0.12-1.el9.noarch.rpm
```

## 🧩 Components

### 📜 Log Rotation
- `/bin/global-logrotate`: Handles automatic rotation and compression of log files

### 🌥️ AWS Operations
- `/bin/global-aws-backup`: Backup files to AWS S3 bucket
- `/bin/global-aws-restore`: Restore files from AWS S3 bucket

### ☁️ Google Cloud Operations
- `/bin/global-gcp-backup`: Backup files to Google Cloud Storage
- `/bin/global-gcp-restore`: Restore files from Google Cloud Storage

## 📖 Usage

### 📜 Log Rotation
```bash
/bin/global-logrotate [options] <log-file-pattern>
```

**Options:**
```
-H                Use full timestamp format (YYYYMMDDTHH:MM:SS)
-D                Use date-only format (YYYYMMDD)
--pattern <glob>  File pattern to rotate (default: *.log)
-p <path>         Specify custom log directory (default: /var/log/apps)
-n                Dry-run mode (no changes made)
-o <path>         Specify old_logs directory (default: <logdir>/old_logs)
--exclude-from <file>  Read a list of file paths or patterns to exclude from rotation
--parallel N      Rotate up to N log files in parallel (default: 4)
```

**Example:**
```bash
/bin/global-logrotate -D --pattern "*.log" -p /var/log/myapp --parallel 4
```

**Output:**
```
2025-08-03: Rotated: /var/log/myapp/app.log -> /var/log/myapp/old_logs/20250803/app.log.20250803.gz
2025-08-03: Rotated: /var/log/myapp/error.log -> /var/log/myapp/old_logs/20250803/error.log.20250803.gz
```

### 🌥️ AWS Backup
```bash
/bin/global-aws-backup --source <path> --destination <s3://bucket> --days <N> [options]
```

**Options:**
```
--source <path>         Source directory to scan for logs
--destination <s3://bucket>  Destination S3 bucket
--days <N>              Only move files older than N days
--pattern <glob>        Optional glob pattern to filter files
--parallel [N]          Move up to N files in parallel (default: 4)
--profile <profile>     AWS CLI profile to use
--region <region>       AWS region to use
```

**Example:**
```bash
/bin/global-aws-backup --source /var/log/apps --destination s3://my-backup/logs --days 30 --pattern "*.log" --parallel 4
```

**Output:**
```
Moving /var/log/apps/app-2025-07-01.log to s3://my-backup/logs/server1/var/log/apps/
Moving /var/log/apps/error-2025-07-01.log to s3://my-backup/logs/server1/var/log/apps/
Moving /var/log/apps/access-2025-07-01.log to s3://my-backup/logs/server1/var/log/apps/
```

### 🌥️ AWS Restore
```bash
/bin/global-aws-restore --source <s3://bucket> --destination <path> [options]
```

**Example:**
```bash
/bin/global-aws-restore --source s3://my-backup/logs --destination /var/log/restore --pattern "*.log"
```

**Output:**
```
Restoring s3://my-backup/logs/server1/var/log/apps/app-2025-07-01.log to /var/log/restore/
Restoring s3://my-backup/logs/server1/var/log/apps/error-2025-07-01.log to /var/log/restore/
Restore completed successfully.
```

### ☁️ GCP Backup
```bash
/bin/global-gcp-backup --source <path> --destination <gs://bucket> --days <N> [options]
```

**Options:**
```
--source <path>         Source directory to scan for logs
--destination <gs://bucket>  Destination GCS bucket
--days <N>              Only move files older than N days
--pattern <glob>        Optional glob pattern to filter files
--parallel [N]          Move up to N files in parallel (default: 4)
```

**Example:**
```bash
/bin/global-gcp-backup --source /var/log/apps --destination gs://my-backup/logs --days 30 --pattern "*.log"
```

**Output:**
```
Moving /var/log/apps/app-2025-07-01.log to gs://my-backup/logs/server1/var/log/apps/
Moving /var/log/apps/error-2025-07-01.log to gs://my-backup/logs/server1/var/log/apps/
Moving /var/log/apps/access-2025-07-01.log to gs://my-backup/logs/server1/var/log/apps/
```

## ⚙️ Requirements

- RHEL/CentOS 9 or compatible distribution
- AWS CLI (for AWS operations)
- Google Cloud SDK (for GCP operations)

## 📝 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 👤 Author

Rushikesh Sakharle