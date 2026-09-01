#!/bin/bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

ssm_deb=/tmp/amazon-ssm-agent.deb
ssm_url="https://s3.${aws_region}.amazonaws.com/amazon-ssm-${aws_region}/latest/debian_amd64/amazon-ssm-agent.deb"

# Bring up the only management path before installing the larger tool set. The
# official Kali cloud image normally includes curl or wget; retain an apt-based
# fallback so a sparse future image can still bootstrap itself.
if command -v curl >/dev/null 2>&1; then
	curl --fail --silent --show-error --location --retry 5 --retry-all-errors \
		"$ssm_url" --output "$ssm_deb"
elif command -v wget >/dev/null 2>&1; then
	wget --tries=5 --output-document="$ssm_deb" "$ssm_url"
else
	apt-get -o Acquire::Retries=5 update
	apt-get -o Acquire::Retries=5 install -y --no-install-recommends ca-certificates curl
	curl --fail --silent --show-error --location --retry 5 --retry-all-errors \
		"$ssm_url" --output "$ssm_deb"
fi

dpkg --install "$ssm_deb"
systemctl enable --now amazon-ssm-agent

apt-get -o Acquire::Retries=5 update
apt-get -o Acquire::Retries=5 install -y --no-install-recommends \
	ca-certificates \
	curl \
	dnsutils \
	impacket-scripts \
	netexec \
	python3-impacket \
	python3-pip

printf '%s\n' '#!/bin/sh' 'exec impacket-secretsdump "$@"' >/usr/local/bin/secretsdump.py
chmod 0755 /usr/local/bin/secretsdump.py

# The Kali marketplace image ships /home/kali owned by root:root; fix it
# before any user-local installers run.
chown -R kali:kali /home/kali

# uv (system-wide) + Dreadnode platform CLI (user-local)
curl -LsSf https://astral.sh/uv/install.sh | UV_INSTALL_DIR=/usr/local/bin sh
su -l kali -c 'curl -fsSL https://dreadnode.io/install.sh | bash'
