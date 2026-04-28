#!/usr/bin/env bash
# Provision 2GB swap on the droplet to protect Node + Mongo from OOM kills.
# Run once on the host (143.198.195.140) as root.
set -euo pipefail

SWAP_FILE="/swapfile"
SWAP_SIZE_GB="${SWAP_SIZE_GB:-2}"

if swapon --show | grep -q "${SWAP_FILE}"; then
  echo "swap already active at ${SWAP_FILE}"
  swapon --show
  exit 0
fi

if [ -e "${SWAP_FILE}" ]; then
  echo "swap file exists but is inactive — activating"
else
  echo "creating ${SWAP_SIZE_GB}G swap file at ${SWAP_FILE}"
  fallocate -l "${SWAP_SIZE_GB}G" "${SWAP_FILE}" || dd if=/dev/zero of="${SWAP_FILE}" bs=1M count=$((SWAP_SIZE_GB*1024)) status=progress
  chmod 600 "${SWAP_FILE}"
  mkswap "${SWAP_FILE}"
fi

swapon "${SWAP_FILE}"

if ! grep -q "^${SWAP_FILE}" /etc/fstab; then
  echo "${SWAP_FILE} none swap sw 0 0" >> /etc/fstab
fi

# Tune for a server with limited RAM but plenty of disk.
sysctl -w vm.swappiness=10
sysctl -w vm.vfs_cache_pressure=50
grep -q '^vm.swappiness' /etc/sysctl.conf || echo 'vm.swappiness=10' >> /etc/sysctl.conf
grep -q '^vm.vfs_cache_pressure' /etc/sysctl.conf || echo 'vm.vfs_cache_pressure=50' >> /etc/sysctl.conf

echo "done"
swapon --show
free -h
