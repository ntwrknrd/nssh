#!/bin/bash
set -e

# Configuration via environment variables
SSH_USER="${SSH_USER:-testuser}"
SSH_PASSWORD="${SSH_PASSWORD:-testpass}"
SSH_AUTH_METHOD="${SSH_AUTH_METHOD:-password}"  # password, key, or both
SSH_AUTHORIZED_KEYS="${SSH_AUTHORIZED_KEYS:-}"

# Generate host keys if they don't exist
if [ ! -f /etc/ssh/ssh_host_rsa_key ]; then
    ssh-keygen -t rsa -b 4096 -f /etc/ssh/ssh_host_rsa_key -N ""
    ssh-keygen -t ed25519 -f /etc/ssh/ssh_host_ed25519_key -N ""
fi

# Create test user if it doesn't exist
if ! id "$SSH_USER" &>/dev/null; then
    adduser -D -s /bin/bash "$SSH_USER"
    echo "$SSH_USER:$SSH_PASSWORD" | chpasswd
fi

# Configure sshd
cat > /etc/ssh/sshd_config <<EOF
Port 22
Protocol 2
HostKey /etc/ssh/ssh_host_rsa_key
HostKey /etc/ssh/ssh_host_ed25519_key

# Logging
SyslogFacility AUTH
LogLevel INFO

# Authentication
LoginGraceTime 120
PermitRootLogin no
StrictModes yes
MaxAuthTries 6

# Password authentication
EOF

case "$SSH_AUTH_METHOD" in
    password)
        echo "PasswordAuthentication yes" >> /etc/ssh/sshd_config
        echo "PubkeyAuthentication no" >> /etc/ssh/sshd_config
        ;;
    key)
        echo "PasswordAuthentication no" >> /etc/ssh/sshd_config
        echo "PubkeyAuthentication yes" >> /etc/ssh/sshd_config
        ;;
    both)
        echo "PasswordAuthentication yes" >> /etc/ssh/sshd_config
        echo "PubkeyAuthentication yes" >> /etc/ssh/sshd_config
        ;;
esac

cat >> /etc/ssh/sshd_config <<EOF

# Other settings
PermitEmptyPasswords no
ChallengeResponseAuthentication no
UsePAM no
X11Forwarding no
PrintMotd no
AcceptEnv LANG LC_*
Subsystem sftp /usr/lib/ssh/sftp-server
EOF

# Set up authorized keys if provided
if [ -n "$SSH_AUTHORIZED_KEYS" ]; then
    mkdir -p "/home/${SSH_USER}/.ssh"
    echo "$SSH_AUTHORIZED_KEYS" > "/home/${SSH_USER}/.ssh/authorized_keys"
    chmod 700 "/home/${SSH_USER}/.ssh"
    chmod 600 "/home/${SSH_USER}/.ssh/authorized_keys"
    chown -R "${SSH_USER}:${SSH_USER}" "/home/${SSH_USER}/.ssh"
fi

# Create a simple shell prompt for demos
cat > "/home/${SSH_USER}/.bashrc" <<'EOF'
PS1='\u@\h:\w\$ '
EOF
chown "${SSH_USER}:${SSH_USER}" "/home/${SSH_USER}/.bashrc"

echo "Starting sshd with auth method: $SSH_AUTH_METHOD"
exec /usr/sbin/sshd -D -e
