Name:           global-sys-utils
Version:        1.0.10
Release:        1%{?dist}
Summary:        System utilities for log rotation and cloud backup
License:        MIT
URL:            https://github.com/rushikesh/global-sys-utils
BuildArch:      noarch
AutoReqProv:    no

Requires:       findutils
Requires:       gzip

%description
A collection of scripts for log rotation, backup, and restore operations 
supporting AWS S3 and Google Cloud Storage, with parallel and pattern 
filtering support.

%prep
# Create directory structure
mkdir -p %{_builddir}/bin
mkdir -p %{_builddir}/man/man1

# Copy scripts to build directory
cp %{_sourcedir}/global-logrotate %{_builddir}/bin/
cp %{_sourcedir}/global-aws-backup %{_builddir}/bin/
cp %{_sourcedir}/global-aws-restore %{_builddir}/bin/
cp %{_sourcedir}/global-gcp-backup %{_builddir}/bin/
cp %{_sourcedir}/global-gcp-restore %{_builddir}/bin/

%install
rm -rf %{buildroot}
mkdir -p %{buildroot}/bin
mkdir -p %{buildroot}%{_mandir}/man1

# Install scripts
install -m 755 %{_builddir}/bin/global-logrotate %{buildroot}/bin/global-logrotate
install -m 755 %{_builddir}/bin/global-aws-backup %{buildroot}/bin/global-aws-backup
install -m 755 %{_builddir}/bin/global-aws-restore %{buildroot}/bin/global-aws-restore
install -m 755 %{_builddir}/bin/global-gcp-backup %{buildroot}/bin/global-gcp-backup
install -m 755 %{_builddir}/bin/global-gcp-restore %{buildroot}/bin/global-gcp-restore

%files
%defattr(-,root,root,-)
/bin/global-logrotate
/bin/global-aws-backup
/bin/global-aws-restore
/bin/global-gcp-backup
/bin/global-gcp-restore

%changelog
* Sat Aug 03 2025 Rushikesh Sakharle <rishiananya123@gmail.com> - 1.0.10-1
- Removed man pages from installation
- Updated file paths and build process
- Added only essential dependencies (findutils, gzip)
- Updated log rotation with improved parallel processing
- Enhanced error handling and reporting