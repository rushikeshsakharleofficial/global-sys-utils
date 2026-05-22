Name:           global-logrotate
Version:        %{_version}
Release:        %{_release}%{?dist}
Summary:        Fast parallel log rotation utility

License:        MIT
URL:            https://github.com/rushikeshsakharleofficial/global-sys-utils

# Go binary is statically compiled (CGO_ENABLED=0) — no glibc/Go runtime deps.
# Python cloud tools require python3 + pip; packages installed in %post.
AutoReqProv:    no

Requires:       python3 >= 3.8
Requires:       python3-pip
Requires(post): systemd
Requires(preun): systemd
Requires(postun): systemd
Recommends:     bash-completion
Suggests:       zsh

%description
global-logrotate is a high-performance log rotation utility written in Go.
It finds log files matching a specified pattern, copies them to a backup
directory with a date suffix, compresses them using gzip, and truncates
the original files. It preserves file ownership and permissions.

Includes cloud backup tools (global-aws-backup, global-aws-restore,
global-gcp-backup, global-gcp-restore) that ship rotated logs to AWS S3
or Google Cloud Storage.

Features:
- Parallel log file processing
- Configurable file patterns
- Configuration file support (/etc/global-sys-utils/)
- Exclude file support
- Dry-run mode
- Daemon mode with cron/interval scheduling and disk monitoring
- AES-256-GCM encryption
- AWS S3 and GCP cloud backup integration

%install
mkdir -p %{buildroot}/usr/bin
mkdir -p %{buildroot}/usr/share/man/man1
mkdir -p %{buildroot}/usr/share/bash-completion/completions
mkdir -p %{buildroot}/usr/share/zsh/vendor-completions
mkdir -p %{buildroot}/etc/global-sys-utils/global.conf.d
mkdir -p %{buildroot}/usr/lib/systemd/system
mkdir -p %{buildroot}/usr/share/global-sys-utils

# Go binary
install -m 755 %{_sourcedir}/global-logrotate        %{buildroot}/usr/bin/

# Python cloud tools
install -m 755 %{_sourcedir}/global-aws-backup        %{buildroot}/usr/bin/
install -m 755 %{_sourcedir}/global-aws-restore       %{buildroot}/usr/bin/
install -m 755 %{_sourcedir}/global-gcp-backup        %{buildroot}/usr/bin/
install -m 755 %{_sourcedir}/global-gcp-restore       %{buildroot}/usr/bin/

# Python requirements (used by postinst pip install)
install -m 644 %{_sourcedir}/requirements.txt         %{buildroot}/usr/share/global-sys-utils/

# Man page + completions
install -m 644 %{_sourcedir}/global-logrotate.1.gz   %{buildroot}/usr/share/man/man1/
install -m 644 %{_sourcedir}/global-logrotate.bash   %{buildroot}/usr/share/bash-completion/completions/%{name}
install -m 644 %{_sourcedir}/_global-logrotate       %{buildroot}/usr/share/zsh/vendor-completions/_%{name}

# Config
install -m 644 %{_sourcedir}/global.conf              %{buildroot}/etc/global-sys-utils/
install -m 644 %{_sourcedir}/example.conf             %{buildroot}/etc/global-sys-utils/global.conf.d/

# Systemd units
install -m 644 %{_sourcedir}/global-logrotate.service      %{buildroot}/usr/lib/systemd/system/
install -m 644 %{_sourcedir}/global-logrotate-once.service %{buildroot}/usr/lib/systemd/system/
install -m 644 %{_sourcedir}/global-logrotate-once.timer   %{buildroot}/usr/lib/systemd/system/

%files
%attr(755, root, root) /usr/bin/%{name}
%attr(755, root, root) /usr/bin/global-aws-backup
%attr(755, root, root) /usr/bin/global-aws-restore
%attr(755, root, root) /usr/bin/global-gcp-backup
%attr(755, root, root) /usr/bin/global-gcp-restore
%attr(644, root, root) /usr/share/man/man1/%{name}.1.gz
%attr(644, root, root) /usr/share/bash-completion/completions/%{name}
%attr(644, root, root) /usr/share/zsh/vendor-completions/_%{name}
%config(noreplace) %attr(644, root, root) /etc/global-sys-utils/global.conf
%config(noreplace) %attr(644, root, root) /etc/global-sys-utils/global.conf.d/example.conf
%dir /etc/global-sys-utils
%dir /etc/global-sys-utils/global.conf.d
%attr(644, root, root) /usr/lib/systemd/system/global-logrotate.service
%attr(644, root, root) /usr/lib/systemd/system/global-logrotate-once.service
%attr(644, root, root) /usr/lib/systemd/system/global-logrotate-once.timer
%attr(644, root, root) /usr/share/global-sys-utils/requirements.txt
%dir /usr/share/global-sys-utils

%post
/usr/bin/mandb -q 2>/dev/null || true
%systemd_post global-logrotate.service global-logrotate-once.timer

# Install Python dependencies for cloud backup tools.
# --break-system-packages is required on RHEL 9+ / Fedora 38+ (PEP 668).
_pip_install() {
    pip3 install --quiet \
        "boto3>=1.34.0" \
        "google-cloud-storage>=2.16.0" \
        "google-auth>=2.28.0" \
        "psutil>=5.9.0" \
        "$@"
}
if command -v pip3 >/dev/null 2>&1; then
    _pip_install --break-system-packages 2>/dev/null || \
    _pip_install 2>/dev/null || \
    echo "Warning: pip install failed. Run: pip3 install -r /usr/share/global-sys-utils/requirements.txt"
else
    echo "Warning: pip3 not found. Install manually: pip3 install -r /usr/share/global-sys-utils/requirements.txt"
fi

%preun
%systemd_preun global-logrotate.service global-logrotate-once.timer

%postun
%systemd_postun_with_restart global-logrotate.service

%changelog
* Thu May 22 2026 Rushikesh Sakharle <rishiananya123@gmail.com> - 2.1.15-1
- Added daemon mode with cron/interval scheduling and disk monitoring
- Added systemd service and timer units
- Added cloud backup integration (AWS S3, GCP GCS) via conf.d
- Added Python cloud tools to package with auto pip install
- Atomic archive writes (temp file + rename before truncation)
- AES-256-GCM encryption hardening
