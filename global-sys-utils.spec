Name:           global-sys-utils
Version:        1.0.11
Release:        1%{?dist}
Summary:        System utilities for log rotation and cloud backup
License:        MIT
URL:            https://github.com/rushikeshsakharleofficial/global-sys-utils
BuildArch:      noarch
AutoReqProv:    no

Requires:       findutils
Requires:       gzip

# Avoid rpmlib dependencies by forcing gzip and disabling build-id links
%define _binary_payload w9.gzdio
%define _source_payload w9.gzdio
%define _build_id_links none

%description
A collection of scripts for log rotation, backup, and restore operations
supporting AWS S3 and Google Cloud Storage, with parallel and pattern
filtering support.

%prep
# Create directory structure
mkdir -p %{_builddir}/bin

# Copy scripts to build directory
cp %{_sourcedir}/global-logrotate %{_builddir}/bin/
cp %{_sourcedir}/global-aws-backup %{_builddir}/bin/
cp %{_sourcedir}/global-aws-restore %{_builddir}/bin/
cp %{_sourcedir}/global-gcp-backup %{_builddir}/bin/
cp %{_sourcedir}/global-gcp-restore %{_builddir}/bin/

%install
rm -rf %{buildroot}
mkdir -p %{buildroot}/bin

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
* Tue Aug 05 2025 Rushikesh Sakharle <rishiananya123@gmail.com> - 1.0.11-1
- Updated version to 1.0.11
- Removed unused man page directories
- Disabled rpmlib dependencies by enforcing gzip payload
- Cleaned up unnecessary build artifacts