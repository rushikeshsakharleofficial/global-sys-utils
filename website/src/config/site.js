/* ============================================================
   SITE CONFIGURATION — edit this file to update site content
   ============================================================ */

export const SITE = {
  /* ── Identity ─────────────────────────────────────────── */
  name:        'global-sys-utils',
  tagline:     'Log Rotation. Cloud Backup. Zero Compromise.',
  description: 'Open-source Linux log management with parallel rotation, AES-256-GCM encryption, AWS S3 & GCS cloud backup, and systemd daemon scheduling.',
  version:     'v2.2.0',
  url:         'https://rushikeshsakharleofficial.github.io/global-sys-utils',
  repo:        'https://github.com/rushikeshsakharleofficial/global-sys-utils',
  author:      'Rushikesh Sakharle',

  /* ── Contact ───────────────────────────────────────────── */
  contact: {
    email:  'ramsharath@instantly.ai',
    issues: 'https://github.com/rushikeshsakharleofficial/global-sys-utils/issues',
  },

  /* ── Social ────────────────────────────────────────────── */
  social: {
    github:  'https://github.com/rushikeshsakharleofficial/global-sys-utils',
    twitter: 'https://twitter.com/rushikeshsakharle',
  },

  /* ── Navigation links ──────────────────────────────────── */
  nav: [
    { label: 'Features',  href: 'features.html' },
    { label: 'Projects',  href: 'projects.html' },
    { label: 'Blog',      href: 'blog.html'      },
    { label: 'Contact',   href: 'contact.html'   },
  ],

  /* ── Hero CTAs ─────────────────────────────────────────── */
  cta: {
    primary:   'Download v2.2.0',
    secondary: 'View on GitHub',
    primaryHref:   'projects.html',
    secondaryHref: 'https://github.com/rushikeshsakharleofficial/global-sys-utils',
  },

  /* ── Features (shown on home & features page) ──────────── */
  features: [
    {
      icon: '⚡',
      title: 'Parallel Rotation',
      body: 'Rotate N log files concurrently, sorted by size for optimal throughput. Configurable worker count via --parallel N.',
    },
    {
      icon: '🔐',
      title: 'AES-256-GCM Encryption',
      body: 'Per-user password stored only as SHA-256 hash. Credentials auto-loaded at runtime with env-var override support.',
    },
    {
      icon: '☁️',
      title: 'AWS S3 Backup',
      body: 'Upload aged archives to S3 with retries, MD5 verification, adaptive upload throttle, and named-profile support.',
    },
    {
      icon: '🌐',
      title: 'GCS Backup',
      body: 'Google Cloud Storage offload with project-level auth, dry-run preview, and automatic ADC credential resolution.',
    },
    {
      icon: '💾',
      title: 'Disk Pressure Guard',
      body: 'Emergency rotation triggers at configurable disk threshold. Archive write guard skips if free space is too low.',
    },
    {
      icon: '🕐',
      title: 'Daemon + systemd',
      body: 'Cron expressions, interval aliases (@daily, 6h), and oneshot timer units. Long-running service with disk monitoring.',
    },
    {
      icon: '📦',
      title: 'DEB + RPM Packages',
      body: 'Pre-built packages for amd64 and arm64. Post-install automatically sets up systemd units and Python dependencies.',
    },
    {
      icon: '🔄',
      title: 'Atomic Writes',
      body: 'Compressed archive written to .tmp then renamed. Crash during rotation leaves source file intact.',
    },
    {
      icon: '📁',
      title: 'conf.d Job System',
      body: 'Each /etc/global-sys-utils/global.conf.d/*.conf is an independent rotation + cloud job in daemon mode.',
    },
  ],

  /* ── Comparison table ──────────────────────────────────── */
  comparison: [
    { capability: 'Parallel rotation',              logrotate: false, gsu: true  },
    { capability: 'AES-256-GCM encryption',         logrotate: false, gsu: true  },
    { capability: 'AWS S3 backup',                  logrotate: false, gsu: true  },
    { capability: 'Google Cloud Storage backup',    logrotate: false, gsu: true  },
    { capability: 'Emergency rotation on disk pressure', logrotate: false, gsu: true },
    { capability: 'Per-file disk space guard',      logrotate: false, gsu: true  },
    { capability: 'Daemon with live scheduling',    logrotate: 'partial', gsu: true },
    { capability: 'systemd service + timer units',  logrotate: 'partial', gsu: true },
    { capability: 'Cron expressions + intervals',   logrotate: 'partial', gsu: true },
    { capability: 'DEB + RPM packages',             logrotate: true,  gsu: true  },
  ],

  /* ── Testimonials ──────────────────────────────────────── */
  testimonials: [
    {
      quote: 'Replaced our cron + logrotate setup in an afternoon. The disk-pressure guard alone saved us twice from full-disk incidents.',
      name:  'Senior SRE',
      org:   'FinTech infrastructure team',
    },
    {
      quote: 'AES-256 encryption and S3 offload out of the box. This is what logrotate should have been ten years ago.',
      name:  'Platform Engineer',
      org:   'Cloud-native startup',
    },
    {
      quote: 'The conf.d job system lets each app team manage their own log policy. Clean separation without central coordination.',
      name:  'Infrastructure Lead',
      org:   'SaaS company, 200+ services',
    },
  ],

  /* ── FAQ ───────────────────────────────────────────────── */
  faq: [
    {
      q: 'Does it replace logrotate entirely?',
      a: 'Yes, for most Linux workloads. global-sys-utils handles rotation, compression, encryption, and cloud offload in a single binary. You can remove the standard logrotate cron and use either the systemd service or your own scheduler.',
    },
    {
      q: 'How does AES-256-GCM encryption work?',
      a: 'Run --pass-gen once to generate and store a password. Only the SHA-256 hash is written to /etc/. At rotation time, the password is read from ~/.global-sys-utils/credentials.ini (mode 0600), or from the LOGROTATE_PASSWORD env var, or prompted interactively.',
    },
    {
      q: 'Can I use both AWS S3 and GCS in the same setup?',
      a: 'Yes. Each conf.d job file specifies its own CLOUD_PROVIDER key (aws or gcp), so you can have some apps backing up to S3 and others to GCS simultaneously in daemon mode.',
    },
    {
      q: 'Does it support ARM64?',
      a: 'Yes. Pre-built .deb and .rpm packages are available for both amd64 and arm64 (aarch64). Download from the installers/ directory or from the GitHub Releases page.',
    },
    {
      q: 'How do I set up the disk-pressure guard?',
      a: 'Set DISK_CRITICAL_PERCENT (default 90) and DISK_MIN_FREE_MB (default 200) in your global.conf. The daemon polls disk usage every DISK_CHECK_INTERVAL seconds (default 60) and triggers emergency rotation when the threshold is crossed.',
    },
    {
      q: 'Is it production-safe?',
      a: 'Yes. All 57 Go tests and 41 Python tests pass with the race detector enabled. Atomic writes protect source files during rotation. Per-file disk guards prevent filling the disk mid-archive. The tool is in active production use.',
    },
  ],

  /* ── Projects / Releases ───────────────────────────────── */
  projects: [
    {
      version: 'v2.2.0',
      date:    'June 2026',
      badge:   'Latest',
      title:   'Adaptive Upload Throttle + conf.d Job System',
      body:    'Concurrent cloud uploads now self-limit based on live CPU/RAM readings. Each conf.d file is treated as an independent daemon job. DEB and RPM packages for amd64 and arm64.',
      deb:     'installers/v2.2.0/global-logrotate_2.2.0-1_amd64.deb',
      debArch: 'amd64',
      rpm:     'installers/v2.2.0/global-logrotate-2.2.0-1.x86_64.rpm',
      rpmArch: 'x86_64',
      tags:    ['cloud', 'daemon', 'packaging'],
    },
    {
      version: 'v2.1.15',
      date:    'May 2026',
      badge:   null,
      title:   'ARM64 Package Support + Retry Jitter',
      body:    'Added aarch64 RPM and arm64 DEB packages. Cloud upload retries now use exponential backoff with random jitter to prevent thundering herd on multi-node setups.',
      deb:     'installers/v2.1.15/global-logrotate_2.1.15-1_arm64.deb',
      debArch: 'arm64',
      rpm:     'installers/v2.1.15/global-logrotate-2.1.15-1.aarch64.rpm',
      rpmArch: 'aarch64',
      tags:    ['packaging', 'arm64', 'cloud'],
    },
  ],

  /* ── Blog posts ─────────────────────────────────────────── */
  blog: [
    {
      slug:  'blog-v2-2-0-release',
      date:  'June 2026',
      badge: 'Latest Release',
      title: 'v2.2.0 — Adaptive Upload Throttle & conf.d Job System',
      body:  'This release introduces intelligent bandwidth management and a modular configuration system designed for high-density production environments. The adaptive throttle prevents OOM on small VMs during peak backup windows.',
      tags:  ['release', 'cloud', 'daemon'],
    },
    {
      slug:  'blog-arm64-support',
      date:  'May 2026',
      title: 'v2.1.15 — ARM64 Package Support',
      body:  'Official support for Apple Silicon and ARM-based Graviton instances. Retry jitter for more reliable cloud uploads.',
      tags:  ['release', 'arm64'],
    },
    {
      slug:  'blog-disk-pressure-guard',
      date:  'Apr 2026',
      title: 'Disk Pressure Guard Deep Dive',
      body:  'Understanding how our new OOM-style disk manager prevents filesystem lockups on space-constrained servers.',
      tags:  ['tutorial', 'disk'],
    },
    {
      slug:  'blog-encryption-walkthrough',
      date:  'Mar 2026',
      title: 'AES-256-GCM Encryption Walkthrough',
      body:  'Securing log payloads before they ever leave the local system. Step-by-step setup guide.',
      tags:  ['security', 'tutorial'],
    },
    {
      slug:  'blog-vs-logrotate',
      date:  'Feb 2026',
      title: 'Comparing logrotate vs global-sys-utils',
      body:  'Why the standard logrotate daemon falls short for high-throughput distributed systems.',
      tags:  ['comparison'],
    },
    {
      slug:  'blog-gcs-backup-gke',
      date:  'Jan 2026',
      title: 'Setting Up GCS Backup on GKE',
      body:  'A step-by-step guide to persistent log storage in Google Cloud Kubernetes.',
      tags:  ['cloud', 'gcp'],
    },
  ],
}
