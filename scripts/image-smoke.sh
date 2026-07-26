#!/usr/bin/env bash
set -euo pipefail
command -v sshd >/dev/null
command -v ssh-keygen >/dev/null
command -v rsync >/dev/null
test -x /opt/gpu-run/remote-runner
test -x /opt/gpu-run/entrypoint
sshd -t -f /etc/ssh/sshd_config
echo "gpu-run image smoke checks passed"
