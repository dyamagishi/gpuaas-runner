#!/usr/bin/env bash
set -euo pipefail
mkdir -p /run/sshd
chmod 0755 /run/sshd
ssh-keygen -A >/dev/null 2>&1
exec /usr/sbin/sshd -D -e -f /etc/ssh/sshd_config
