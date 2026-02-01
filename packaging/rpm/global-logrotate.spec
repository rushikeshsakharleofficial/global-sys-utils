Name:           global-logrotate
Version:        %{_version}
Release:        %{_release}%{?dist}
Summary:        Fast parallel log rotation utility

License:        MIT
URL:            https://github.com/rushikeshsakharleofficial/global-sys-utils

# Binary is pre-compiled, no build needed
AutoReqProv:    no

Recommends:     bash-completion
Suggests:       zsh

%description
global-logrotate is a high-performance log rotation utility written in Go.
It finds log files matching a specified pattern, copies them to a backup
directory with a date suffix, compresses them using gzip, and truncates
the original files. It preserves file ownership and permissions.

Features:
- Parallel log file processing
- Configurable file patterns
- Configuration file support (/etc/global-sys-utils/)
- Exclude file support
- Dry-run mode
- Preserves file permissions and ownership

%install
mkdir -p %{buildroot}/usr/bin
mkdir -p %{buildroot}/usr/share/man/man1
mkdir -p %{buildroot}/usr/share/bash-completion/completions
mkdir -p %{buildroot}/usr/share/zsh/vendor-completions
mkdir -p %{buildroot}/etc/global-sys-utils/global.conf.d
install -m 755 %{_sourcedir}/global-logrotate %{buildroot}/usr/bin/
install -m 644 %{_sourcedir}/global-logrotate.1.gz %{buildroot}/usr/share/man/man1/
install -m 644 %{_sourcedir}/global-logrotate.bash %{buildroot}/usr/share/bash-completion/completions/%{name}
install -m 644 %{_sourcedir}/_global-logrotate %{buildroot}/usr/share/zsh/vendor-completions/_%{name}
install -m 644 %{_sourcedir}/global.conf %{buildroot}/etc/global-sys-utils/
install -m 644 %{_sourcedir}/example.conf %{buildroot}/etc/global-sys-utils/global.conf.d/

%files
%attr(755, root, root) /usr/bin/%{name}
%attr(644, root, root) /usr/share/man/man1/%{name}.1.gz
%attr(644, root, root) /usr/share/bash-completion/completions/%{name}
%attr(644, root, root) /usr/share/zsh/vendor-completions/_%{name}
%config(noreplace) %attr(644, root, root) /etc/global-sys-utils/global.conf
%config(noreplace) %attr(644, root, root) /etc/global-sys-utils/global.conf.d/example.conf
%dir /etc/global-sys-utils
%dir /etc/global-sys-utils/global.conf.d

%post
/usr/bin/mandb -q 2>/dev/null || true

%changelog
* Sat Feb 01 2026 Rushikesh Sakharle <rushikesh.sakharle@linuxhardened.com> - 2.1.15-1
- Removed overwrite option from --pass-gen for security
- --pass-gen only for initial setup, --pass-reset for changes

* Sat Feb 01 2026 Rushikesh Sakharle <rushikesh.sakharle@linuxhardened.com> - 2.1.14-1
- Password auto-loaded from ~/.global-sys-utils/config/credentials.ini
- Users without credentials file are prompted for password
- Each user configures their own password via --pass-gen

* Sat Feb 01 2026 Rushikesh Sakharle <rushikesh.sakharle@linuxhardened.com> - 2.1.12-1
- Rewritten in Go for improved performance
- Added configuration file support (/etc/global-sys-utils/global.conf)
- Added drop-in configuration directory (/etc/global-sys-utils/global.conf.d/)
- Initial binary release (converted from shell script)
