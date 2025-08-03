# 🛠️ global-sys-utils

A collection of scripts for log rotation, backup, and restore operations supporting AWS S3 and Google Cloud Storage, with parallel and pattern filtering support.

## ✨ Features

- 📜 **Log Rotation**: Automated log file rotation and compression
- ☁️ **Cloud Backup**: Support for AWS S3 and Google Cloud Storage
- 🔄 **Restore Operations**: Easy restoration of backed up files
- 🔍 **Pattern Filtering**: Selective backup/restore using pattern matching
- ⚡ **Parallel Processing**: Improved performance through parallel operations

## 📥 Installation

The package is available as an RPM for RHEL/CentOS 9:

```bash
rpm -ivh RPMS/noarch/global-sys-utils-1.0.0-1.el9.noarch.rpm
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

### 🌥️ AWS Backup
```bash
/bin/global-aws-backup [options] --bucket <bucket-name> <source-path>
```

### 🌥️ AWS Restore
```bash
/bin/global-aws-restore [options] --bucket <bucket-name> <destination-path>
```

### ☁️ GCP Backup
```bash
/bin/global-gcp-backup [options] --bucket <bucket-name> <source-path>
```

### ☁️ GCP Restore
```bash
/bin/global-gcp-restore [options] --bucket <bucket-name> <destination-path>
```

## ⚙️ Requirements

- RHEL/CentOS 9 or compatible distribution
- AWS CLI (for AWS operations)
- Google Cloud SDK (for GCP operations)

## 📝 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 👤 Author

Rushikesh Sakharle