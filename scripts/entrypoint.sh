#!/usr/bin/env bash
set -euo pipefail
mkdir -p /run/sshd
chmod 0755 /run/sshd
if [[ -n "${PUBLIC_KEY:-}" ]]; then
  install -d -m 0700 /root/.ssh
  printf '%s\n' "$PUBLIC_KEY" | awk '/^(ssh-(rsa|ed25519|ecdsa)|ecdsa-)/ && NF >= 2 { print }' > /root/.ssh/authorized_keys
  chmod 0600 /root/.ssh/authorized_keys
fi
ssh-keygen -A >/dev/null 2>&1
exec /usr/sbin/sshd -D -e -f /etc/ssh/sshd_config
