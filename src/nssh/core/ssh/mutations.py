"""Pure helpers for manipulating SSH host entries."""

from __future__ import annotations

from pathlib import Path
from typing import List, Optional, Sequence, Tuple

from nssh.core.ssh.config import SSHConfigParser
from nssh.core.ssh.fixer import AUTH_CONFIGS, COMPAT_CONFIGS


class HostUpdateError(RuntimeError):
    """Raised when an SSH host cannot be updated."""


def render_host_update(
    host_lines: List[str],
    *,
    auth_type: Optional[str] = None,
    compat_types: Optional[Sequence[str]] = None,
) -> List[str]:
    """Return updated host lines with requested auth/compat tweaks."""

    compat_types = list(compat_types or [])

    compat_directive_map = {
        "kex": "KexAlgorithms",
        "macs": "MACs",
        "ciphers": "Ciphers",
        "hostkey": "HostKeyAlgorithms",
    }

    directives_to_remove = []
    if compat_types:
        directives_to_remove = [
            compat_directive_map[ct]
            for ct in compat_types
            if ct in compat_directive_map
        ]

    new_lines: List[str] = []
    auth_inserted = False
    compat_inserted = False

    for line in host_lines:
        stripped = line.strip()

        if auth_type and any(
            pattern in line
            for pattern in [
                "PreferredAuthentications",
                "PubkeyAuthentication",
                "PasswordAuthentication",
            ]
        ):
            continue

        if directives_to_remove and any(
            stripped.startswith(f"{directive} ") for directive in directives_to_remove
        ):
            continue

        new_lines.append(line)

        if auth_type and stripped.startswith("User ") and not auth_inserted:
            new_lines.extend(AUTH_CONFIGS[auth_type]["config_lines"])
            auth_inserted = True

        if (
            compat_types
            and (stripped.startswith("HostName ") or stripped.startswith("Port "))
            and not compat_inserted
        ):
            for compat_type in compat_types:
                new_lines.extend(COMPAT_CONFIGS[compat_type]["config_lines"])
            compat_inserted = True

    if auth_type and not auth_inserted:
        insert_idx = -1
        for i, line in enumerate(new_lines):
            stripped = line.strip()
            if stripped.startswith("HostName ") or stripped.startswith("Port "):
                insert_idx = i + 1

        if insert_idx > 0:
            for auth_line in reversed(AUTH_CONFIGS[auth_type]["config_lines"]):
                new_lines.insert(insert_idx, auth_line)

    if compat_types and not compat_inserted:
        insert_idx = 1
        for compat_type in reversed(compat_types):
            for compat_line in reversed(COMPAT_CONFIGS[compat_type]["config_lines"]):
                new_lines.insert(insert_idx, compat_line)

    return new_lines


def apply_host_update(
    parser: SSHConfigParser,
    hostname: str,
    *,
    auth_type: Optional[str] = None,
    compat_types: Optional[Sequence[str]] = None,
    create_backup: bool = True,
) -> Tuple[Path, Path]:
    """Apply the rendered update to disk, returning (target_file, backup_path)."""

    result = parser.find_host_in_files(hostname)
    if not result:
        raise HostUpdateError(f"Host '{hostname}' not found")

    target_file, host_lines = result
    new_host_lines = render_host_update(
        host_lines, auth_type=auth_type, compat_types=compat_types
    )

    backup_path = parser.create_backup(target_file) if create_backup else target_file

    header_lines, hosts = parser.parse_ssh_config(target_file)
    updated_hosts = []
    for host_name, lines in hosts:
        if host_name == hostname:
            updated_hosts.append((host_name, new_host_lines))
        else:
            updated_hosts.append((host_name, lines))

    parser.write_ssh_config(target_file, header_lines, updated_hosts)
    parser.rebuild_index()

    return target_file, backup_path
