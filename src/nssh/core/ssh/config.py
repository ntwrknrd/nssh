#!/usr/bin/env python3
"""
SSH config file parsing and manipulation module
"""

import glob
import os
import re
import shlex
import shutil
from datetime import datetime
from pathlib import Path
from typing import List, Tuple, Optional

from nssh.core.env.paths import (
    backup_directory,
    host_index_path,
    ssh_config_path,
)
from nssh.core.env.settings import get_config
from nssh.core.env.system import set_secure_permissions


NSSH_BACKUP_DIR: Path | None = None


def _resolved_backup_dir() -> Path:
    return NSSH_BACKUP_DIR or backup_directory()


class SSHConfigParser:
    """Parse and manipulate SSH config files"""

    _INCLUDE_PATTERN = re.compile(r"^include\s+(.+)$", re.IGNORECASE)

    def __init__(
        self, config_file: Optional[Path] = None, backup_dir: Optional[Path] = None
    ):
        self.config_file = config_file or ssh_config_path()
        self.backup_dir = backup_dir or _resolved_backup_dir()

    @staticmethod
    def _split_include_targets(targets: str) -> List[str]:
        try:
            return shlex.split(targets)
        except ValueError:
            return targets.split()

    def _resolve_include_target(self, raw_path: str) -> List[Path]:
        base_dir = self.config_file.parent if self.config_file else Path.home()
        expanded = os.path.expanduser(raw_path)
        if not os.path.isabs(expanded):
            expanded = os.path.join(str(base_dir), expanded)

        matches = glob.glob(expanded)
        candidates = matches if matches else [expanded]
        resolved: List[Path] = []
        for candidate in candidates:
            path = Path(candidate).expanduser()
            try:
                normalized = path.resolve()
            except OSError:
                normalized = path
            if normalized.is_file():
                resolved.append(normalized)
        return resolved

    @staticmethod
    def _split_hostnames(host_line: str) -> List[str]:
        text = host_line.strip()
        if not text.lower().startswith("host "):
            return []
        remainder = text[5:].strip()
        if not remainder:
            return []
        return remainder.split()

    @staticmethod
    def extract_aliases(host_lines: List[str]) -> List[str]:
        if not host_lines:
            return []
        return SSHConfigParser._split_hostnames(host_lines[0])

    def find_include_files(self) -> List[Path]:
        """
        Scan ~/.ssh/config for Include lines and return file paths

        Returns:
            List[Path]: List of included config file paths that exist
        """
        if not self.config_file.exists():
            return []

        include_files: List[Path] = []
        seen = set()

        with open(self.config_file, "r") as f:
            for line in f:
                stripped = line.strip()

                # Skip comments
                if stripped.startswith("#"):
                    continue

                match = self._INCLUDE_PATTERN.match(stripped)
                if not match:
                    continue

                targets = self._split_include_targets(match.group(1))
                for target in targets:
                    for candidate in self._resolve_include_target(target):
                        if candidate not in seen:
                            include_files.append(candidate)
                            seen.add(candidate)

        return include_files

    def parse_ssh_config(
        self, file_path: Path
    ) -> Tuple[List[str], List[Tuple[str, List[str]]]]:
        """
        Parse SSH config file into header and host entries

        Args:
            file_path: Path to SSH config file

        Returns:
            Tuple of:
            - header_lines: List of lines before first Host entry
            - hosts: List of (hostname, lines) tuples
        """
        if not file_path.exists():
            return [], []

        with open(file_path, "r") as f:
            lines = f.readlines()

        header_lines: List[str] = []
        hosts: List[Tuple[str, List[str]]] = []

        current_host: Optional[str] = None
        current_host_lines: List[str] = []
        in_header = True

        for line in lines:
            stripped = line.strip()

            # Check for Host directive
            if stripped.lower().startswith("host "):
                # Save previous host if exists
                if current_host is not None:
                    hosts.append((current_host, current_host_lines))

                # Start new host
                in_header = False
                alias_list = self._split_hostnames(line)
                if not alias_list:
                    continue
                host_name = alias_list[0]

                # Skip wildcard hosts (treat as header)
                if host_name.startswith("*"):
                    if in_header or not hosts:
                        header_lines.append(line)
                        current_host = None
                        current_host_lines = []
                    continue

                current_host = host_name
                current_host_lines = [line]
            else:
                # Add line to current context
                if in_header:
                    header_lines.append(line)
                elif current_host is not None:
                    current_host_lines.append(line)

        # Save last host
        if current_host is not None:
            hosts.append((current_host, current_host_lines))

        return header_lines, hosts

    def find_insertion_index(
        self, hosts: List[Tuple[str, List[str]]], new_hostname: str
    ) -> int:
        """
        Find the index where new host should be inserted to maintain alphabetical order

        Args:
            hosts: List of (hostname, lines) tuples
            new_hostname: Hostname to insert

        Returns:
            int: Index where host should be inserted
        """
        for i, (hostname, _) in enumerate(hosts):
            if new_hostname.lower() < hostname.lower():
                return i
        return len(hosts)

    def write_ssh_config(
        self,
        file_path: Path,
        header_lines: List[str],
        hosts: List[Tuple[str, List[str]]],
    ) -> bool:
        """
        Write header and hosts back to SSH config file

        Args:
            file_path: Path to SSH config file
            header_lines: Lines to write at the beginning
            hosts: List of (hostname, lines) tuples

        Returns:
            bool: True if successful
        """
        try:
            with open(file_path, "w") as f:
                # Write header
                f.writelines(header_lines)

                # Write hosts
                for hostname, host_lines in hosts:
                    f.writelines(host_lines)

            # Set secure permissions
            set_secure_permissions(file_path)

            return True

        except Exception as e:
            raise RuntimeError(f"Failed to write SSH config: {e}")

    def create_backup(self, file_path: Path) -> Path:
        """
        Create timestamped backup of file

        Args:
            file_path: Path to file to backup

        Returns:
            Path: Path to backup file
        """
        self.backup_dir.mkdir(exist_ok=True, parents=True)
        try:
            self.backup_dir.chmod(0o700)
        except OSError:
            # Directory may be on a filesystem that rejects chmod; continue anyway
            pass

        timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
        backup_name = f"{file_path.name}.{timestamp}.bak"
        backup_path = self.backup_dir / backup_name

        shutil.copy2(file_path, backup_path)
        set_secure_permissions(backup_path)

        self._prune_backups(file_path.name)
        return backup_path

    def _prune_backups(self, source_name: str) -> None:
        """Remove old backups exceeding the configured limit.

        Args:
            source_name: Original filename (e.g., 'config' or 'homelab')
        """
        max_files = get_config().backup.max_files
        if max_files <= 0:
            return  # No limit configured

        # Find all backups for this source file: {source_name}.*.bak
        pattern = f"{source_name}.*.bak"
        backups = list(self.backup_dir.glob(pattern))

        if len(backups) <= max_files:
            return

        # Sort by modification time, newest first
        backups.sort(key=lambda p: p.stat().st_mtime, reverse=True)

        # Remove oldest backups beyond the limit
        for stale in backups[max_files:]:
            stale.unlink(missing_ok=True)

    def find_host_in_files(
        self, hostname: str, include_files: Optional[List[Path]] = None
    ) -> Optional[Tuple[Path, List[str]]]:
        """
        Find which Include file contains a hostname

        Args:
            hostname: Hostname to find
            include_files: Optional list of files to search (defaults to all Include files)

        Returns:
            Tuple of (file_path, host_lines) if found, None otherwise
        """
        if include_files is None:
            include_files = self.find_include_files()

        for file_path in include_files:
            _, hosts = self.parse_ssh_config(file_path)

            for host_name, host_lines in hosts:
                aliases = self.extract_aliases(host_lines)
                if hostname in aliases:
                    return file_path, host_lines

        return None

    def find_host_by_hostname(
        self, target_hostname: str, include_files: Optional[List[Path]] = None
    ) -> Optional[Tuple[Path, str, List[str]]]:
        """
        Find a host entry by its Hostname directive value.

        This searches for hosts where the Hostname directive matches the target,
        useful when batch files contain FQDNs but SSH config uses short aliases.

        Args:
            target_hostname: The Hostname value to search for (e.g., 'k3s-d.home.arpa')
            include_files: Optional list of files to search (defaults to all Include files)

        Returns:
            Tuple of (file_path, host_alias, host_lines) if found, None otherwise
        """
        if include_files is None:
            include_files = self.find_include_files()

        hostname_pattern = re.compile(r"^\s*hostname\s+(.+)$", re.IGNORECASE)

        for file_path in include_files:
            _, hosts = self.parse_ssh_config(file_path)

            for host_alias, host_lines in hosts:
                for line in host_lines:
                    match = hostname_pattern.match(line)
                    if match:
                        configured_hostname = match.group(1).strip()
                        if configured_hostname == target_hostname:
                            return file_path, host_alias, host_lines

        return None

    def find_all_hosts_by_hostname(
        self, target_hostname: str, include_files: Optional[List[Path]] = None
    ) -> List[Tuple[Path, str, List[str]]]:
        """
        Find all host entries matching a Hostname directive across all config files.

        Unlike find_host_by_hostname which returns the first match, this returns
        all matches for removing hosts that appear in multiple contexts.

        Args:
            target_hostname: The Hostname value to search for (e.g., 'test.domain.local')
            include_files: Optional list of files to search (defaults to all Include files)

        Returns:
            List of (file_path, host_alias, host_lines) tuples.
        """
        if include_files is None:
            include_files = self.find_include_files()

        hostname_pattern = re.compile(r"^\s*hostname\s+(.+)$", re.IGNORECASE)
        matches: List[Tuple[Path, str, List[str]]] = []

        for file_path in include_files:
            _, hosts = self.parse_ssh_config(file_path)

            for host_alias, host_lines in hosts:
                for line in host_lines:
                    match = hostname_pattern.match(line)
                    if match:
                        configured_hostname = match.group(1).strip()
                        if configured_hostname == target_hostname:
                            matches.append((file_path, host_alias, host_lines))
                            break  # Found hostname in this host block

        return matches

    def find_all_hosts_by_alias(
        self, alias: str, include_files: Optional[List[Path]] = None
    ) -> List[Tuple[Path, str, List[str], str]]:
        """
        Find all host entries matching a given alias across all config files.

        Unlike find_host_in_files which returns the first match, this returns
        all matches to detect ambiguous cases where multiple hosts share the
        same short name.

        Args:
            alias: Host alias to find (e.g., 'test-host')
            include_files: Optional list of files to search (defaults to all Include files)

        Returns:
            List of (file_path, host_alias, host_lines, hostname_fqdn) tuples.
            hostname_fqdn is extracted from the Hostname directive if present.
        """
        if include_files is None:
            include_files = self.find_include_files()

        hostname_pattern = re.compile(r"^\s*hostname\s+(.+)$", re.IGNORECASE)
        matches: List[Tuple[Path, str, List[str], str]] = []

        for file_path in include_files:
            _, hosts = self.parse_ssh_config(file_path)

            for host_alias, host_lines in hosts:
                aliases = self.extract_aliases(host_lines)
                if alias in aliases:
                    # Extract Hostname directive if present
                    hostname_fqdn = ""
                    for line in host_lines:
                        match = hostname_pattern.match(line)
                        if match:
                            hostname_fqdn = match.group(1).strip()
                            break
                    matches.append((file_path, host_alias, host_lines, hostname_fqdn))

        return matches

    def host_exists(
        self,
        file_path: Path,
        hostname: str,
        parsed_hosts: Optional[List[Tuple[str, List[str]]]] = None,
    ) -> bool:
        """
        Check if hostname already exists in config file

        Args:
            file_path: Path to SSH config file
            hostname: Hostname to check
            parsed_hosts: Optional pre-parsed hosts to avoid re-parsing

        Returns:
            bool: True if hostname exists
        """
        if parsed_hosts is None:
            _, parsed_hosts = self.parse_ssh_config(file_path)

        for _, host_lines in parsed_hosts:
            aliases = self.extract_aliases(host_lines)
            if hostname in aliases:
                return True
        return False

    def get_surrounding_hosts(
        self, hosts: List[Tuple[str, List[str]]], index: int, context: int = 2
    ) -> Tuple[List[str], List[str]]:
        """
        Get hostnames before and after insertion point

        Args:
            hosts: List of (hostname, lines) tuples
            index: Insertion index
            context: Number of hosts to show on each side

        Returns:
            Tuple of (before_hostnames, after_hostnames)
        """
        before = []
        after = []

        # Get hosts before
        for i in range(max(0, index - context), index):
            before.append(hosts[i][0])

        # Get hosts after
        for i in range(index, min(len(hosts), index + context)):
            after.append(hosts[i][0])

        return before, after

    def rebuild_index(self) -> Path:
        """
        Rebuild the host index file for fast lookups

        Creates ~/.ssh/.nssh_host_index with format: hostname|filepath
        This index is used by nssh-select for fast exact-match lookups,
        avoiding the need to parse all SSH config files.

        Returns:
            Path: Path to the created index file

        Raises:
            RuntimeError: If index creation fails
        """
        index_path = host_index_path()

        try:
            # Get all config files
            config_files = [self.config_file]
            config_files.extend(self.find_include_files())

            # Ensure index directory exists
            index_path.parent.mkdir(parents=True, exist_ok=True)

            # Open index file for writing
            with open(index_path, "w") as index_file:
                # Parse each config file and write hosts to index
                for config_file in config_files:
                    if not config_file.exists():
                        continue

                    _, hosts = self.parse_ssh_config(config_file)

                    for hostname, host_lines in hosts:
                        aliases = self.extract_aliases(host_lines) or [hostname]
                        for alias in aliases:
                            # Skip wildcard hosts (already filtered by parse_ssh_config)
                            # But double-check for ? wildcards
                            if "?" in alias or "*" in alias:
                                continue

                            index_file.write(f"{alias}|{config_file}\n")

            # Set secure permissions
            set_secure_permissions(index_path)

            return index_path

        except Exception as e:
            raise RuntimeError(f"Failed to rebuild host index: {e}")


if __name__ == "__main__":
    # Simple test
    parser = SSHConfigParser()
    includes = parser.find_include_files()
    print(f"Found {len(includes)} include files:")
    for inc in includes:
        print(f"  {inc}")
