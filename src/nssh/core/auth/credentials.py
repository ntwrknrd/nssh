#!/usr/bin/env python3
"""
Credential management module for nssh
Handles age-encrypted multi-credential storage with context support
"""

import json
import os
import shutil
import tempfile
import time
from contextlib import contextmanager
from datetime import datetime
from pathlib import Path
from typing import Optional, Dict, List, Tuple, Iterator

from nssh.core.env.paths import age_key_path, credential_file_path
from nssh.core.env.system import check_command, run_command, set_secure_permissions


_BACKUP_LIMIT = 5
_LOCK_TIMEOUT_SECONDS = 5.0


class CredentialManager:
    """Manages age-encrypted credentials in ~/.ssh/nssh_credentials.age"""

    def __init__(
        self, credential_file: Optional[Path] = None, age_key: Optional[Path] = None
    ):
        self.credential_file = credential_file or credential_file_path()
        self.age_key = age_key or age_key_path()
        self._cache_data: Optional[Dict] = None
        self._cache_mtime: Optional[float] = None
        self._public_key: Optional[str] = None
        self._age_key_mtime: Optional[float] = None
        self._lock_timeout = _LOCK_TIMEOUT_SECONDS

        # Verify age is installed
        if not check_command("age"):
            raise RuntimeError(
                "'age' command not found. Install with: brew install age"
            )

        # Verify age key exists
        if not self.age_key.exists():
            raise RuntimeError(f"Age key not found at {self.age_key}")

    # ============================================
    # Internal helpers
    # ============================================

    def _empty_structure(self) -> Dict:
        """Return a new empty credential structure"""
        return {"contexts": {}, "hosts": {}}

    def _normalize_git_include_file(self, include_value: Optional[str]) -> str:
        """Return the basename portion of an Include file reference."""

        if not include_value:
            return ""

        value = include_value.strip()
        if not value:
            return ""

        try:
            normalized = Path(value).name
        except Exception:
            return value

        return normalized or value

    def _extract_context_credential(self, ctx_data: Dict) -> Optional[Dict]:
        """Return the single credential dict for a context."""

        return ctx_data.get("credential")

    def _write_context_credential(
        self, ctx_data: Dict, username: str, password: str
    ) -> None:
        """Persist a context credential."""

        ctx_data["credential"] = {"username": username, "password": password}

    def _lock_dir(self) -> Path:
        return Path(f"{self.credential_file}.lock")

    @contextmanager
    def _exclusive_lock(self) -> Iterator[None]:
        lock_dir = self._lock_dir()
        deadline = time.monotonic() + self._lock_timeout
        while True:
            try:
                lock_dir.mkdir(mode=0o700, exist_ok=False)
                info_path = lock_dir / ".lockinfo"
                info_path.write_text(f"pid={os.getpid()}\n", encoding="utf-8")
                break
            except FileExistsError:
                if time.monotonic() >= deadline:
                    holder = ""
                    info_path = lock_dir / ".lockinfo"
                    if info_path.exists():
                        holder = info_path.read_text(encoding="utf-8").strip()
                    raise RuntimeError(
                        "Credential store is busy. "
                        f"Lock held at {lock_dir} {f'({holder})' if holder else ''}"
                    )
                time.sleep(0.1)
        try:
            yield
        finally:
            shutil.rmtree(lock_dir, ignore_errors=True)

    def _backup_credentials(self) -> None:
        if not self.credential_file.exists():
            return
        timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
        backup_name = f"{self.credential_file.name}.bak.{timestamp}"
        backup_path = self.credential_file.parent / backup_name
        shutil.copy2(self.credential_file, backup_path)
        self._prune_backups()

    def _prune_backups(self) -> None:
        pattern = f"{self.credential_file.name}.bak."
        backups = [
            path
            for path in self.credential_file.parent.glob(
                f"{self.credential_file.name}.bak.*"
            )
            if path.name.startswith(pattern)
        ]
        backups.sort(key=lambda p: p.stat().st_mtime, reverse=True)
        for stale in backups[_BACKUP_LIMIT:]:
            stale.unlink(missing_ok=True)

    def _load_fresh_credentials(self) -> Dict:
        data = self._load_credentials_from_disk()
        self._cache_data = data
        self._cache_mtime = self._credential_file_mtime()
        return data

    def _credential_file_mtime(self) -> Optional[float]:
        """Get credential file modification time (None if missing)"""
        try:
            return self.credential_file.stat().st_mtime
        except FileNotFoundError:
            return None

    def _age_key_mtime_current(self) -> float:
        """Return the current mtime for the age key (raises if missing)"""
        return self.age_key.stat().st_mtime

    def _ensure_public_key(self) -> str:
        """Ensure the age public key is cached and up to date"""
        current_mtime = self._age_key_mtime_current()

        if self._public_key is None or self._age_key_mtime != current_mtime:
            key_result = run_command(
                ["age-keygen", "-y", str(self.age_key)],
                error_context="Failed to get public key",
            )
            self._public_key = key_result.stdout.strip()
            self._age_key_mtime = current_mtime

        return self._public_key

    def _load_credentials_from_disk(self) -> Dict:
        """Decrypt credential file from disk (or return empty structure)"""
        if not self.credential_file.exists():
            return self._empty_structure()

        try:
            result = run_command(
                ["age", "-d", "-i", str(self.age_key), str(self.credential_file)],
                error_context="Failed to decrypt credentials",
            )

            data = json.loads(result.stdout)

            # Ensure keys exist
            if "contexts" not in data:
                data["contexts"] = {}
            if "hosts" not in data:
                data["hosts"] = {}

            return data

        except json.JSONDecodeError as e:
            raise RuntimeError(f"Invalid JSON in credential file: {e}")

    def _ensure_decrypted(self) -> Dict:
        """Ensure decrypted credentials are cached and fresh"""
        current_mtime = self._credential_file_mtime()
        cache_stale = self._cache_mtime != current_mtime

        if self._cache_data is None or cache_stale:
            data = self._load_credentials_from_disk()
            self._cache_data = data
            self._cache_mtime = current_mtime

        return self._cache_data

    def decrypt_credentials(self) -> dict:
        """
        Decrypt network_passwords.age and return JSON dict

        Format: {"contexts": {...}, "hosts": {...}}

        Returns:
            dict: Credential data structure
        """
        return self._ensure_decrypted()

    def _write_credentials_locked(self, data: dict) -> bool:
        if not isinstance(data.get("contexts"), dict) or not isinstance(
            data.get("hosts"), dict
        ):
            raise ValueError(
                "Invalid data structure: 'contexts' and 'hosts' must be dicts"
            )

        json_data = json.dumps(data, indent=2)
        public_key = self._ensure_public_key()

        with tempfile.NamedTemporaryFile(mode="w", delete=False, suffix=".age") as tmp:
            tmp_path = tmp.name

        try:
            run_command(
                ["age", "-r", public_key, "-o", tmp_path],
                input_text=json_data,
                error_context="Failed to encrypt credentials",
            )
            self._backup_credentials()
            os.replace(tmp_path, self.credential_file)
            set_secure_permissions(self.credential_file)
            self._cache_data = data
            self._cache_mtime = self._credential_file_mtime()
            return True
        except RuntimeError:
            if os.path.exists(tmp_path):
                os.remove(tmp_path)
            raise

    def encrypt_credentials(self, data: dict) -> bool:
        """
        Encrypt dict to network_passwords.age (with locking/backups).
        """
        with self._exclusive_lock():
            return self._write_credentials_locked(data)

    def get_context(self, git_include_file: str) -> Optional[Dict]:
        """
        Get context by git include file name

        Args:
            git_include_file: Name of SSH config include file (e.g., "work_hosts")

        Returns:
            dict | None: Context data if found, None otherwise
        """
        if not git_include_file:
            return None

        data = self.decrypt_credentials()
        requested_raw = git_include_file.strip()
        requested_normalized = self._normalize_git_include_file(requested_raw)

        for ctx_name, ctx_data in data.get("contexts", {}).items():
            stored_raw = ctx_data.get("git_include_file", "")
            stored_normalized = self._normalize_git_include_file(stored_raw)
            matches = False

            if stored_raw == requested_raw:
                matches = True
            elif stored_normalized and stored_normalized == requested_normalized:
                matches = True

            if matches:
                credential = self._extract_context_credential(ctx_data)
                return {
                    "name": ctx_name,
                    "git_include_file": stored_raw,
                    "credential": credential,
                }

        return None

    def get_host_credentials(self, hostname: str) -> Optional[List[Dict]]:
        """
        Get all credentials for a specific host

        Args:
            hostname: Hostname to look up

        Returns:
            list[dict] | None: List of credentials if found, None otherwise
        """
        data = self.decrypt_credentials()

        host_data = data.get("hosts", {}).get(hostname)
        if host_data and isinstance(host_data, dict):
            credentials = host_data.get("credentials", [])
            return credentials if credentials else None

        return None

    def resolve_credential(
        self,
        hostname: str,
        git_include_file: Optional[str] = None,
        username: Optional[str] = None,
    ) -> Optional[Tuple[str, str]]:
        """
        Resolve credential using resolution algorithm

        Algorithm:
        If username specified:
          1. Search hosts[hostname].credentials for username match
          2. Search context.credentials for username match (if context exists)
          3. Return None if not found

        If username not specified:
          1. Use hosts[hostname].credentials[0] if exists
          2. Use context.credentials[0] if context exists
          3. Return None

        Args:
            hostname: Hostname to resolve
            git_include_file: Name of SSH config include file for context lookup
            username: Optional username to match

        Returns:
            tuple[str, str] | None: (username, password) if found, None otherwise
        """
        # Get potential credential sources
        host_creds = self.get_host_credentials(hostname)

        context_cred = None
        if git_include_file:
            context = self.get_context(git_include_file)
            context_cred = context["credential"] if context else None

        # If username specified, find exact match
        if username:
            # Check host-specific credentials
            if host_creds:
                for cred in host_creds:
                    if cred["username"] == username:
                        return (cred["username"], cred["password"])

            # Check context credentials
            if context_cred and context_cred["username"] == username:
                return (context_cred["username"], context_cred["password"])

            return None

        # No username specified: use first available
        if host_creds:
            return (host_creds[0]["username"], host_creds[0]["password"])

        if context_cred:
            return (context_cred["username"], context_cred["password"])

        return None

    # ============================================
    # Context Management Methods
    # ============================================

    def create_context(self, name: str, git_include_file: str) -> bool:
        """
        Create a new context

        Args:
            name: Context name
            git_include_file: SSH config include file name

        Returns:
            bool: True if successful
        """
        with self._exclusive_lock():
            data = self._load_fresh_credentials()

            if name in data["contexts"]:
                raise ValueError(f"Context '{name}' already exists")

            normalized_include = self._normalize_git_include_file(git_include_file)
            stored_include = normalized_include or git_include_file

            data["contexts"][name] = {
                "git_include_file": stored_include,
                "credential": None,
            }

            return self._write_credentials_locked(data)

    def add_context_credential(
        self, name: str, username: str, password: str, *, overwrite: bool = False
    ) -> bool:
        """
        Add credential to context

        Args:
            name: Context name
            username: Username
            password: Password

        Returns:
            bool: True if successful
        """
        with self._exclusive_lock():
            data = self._load_fresh_credentials()

            if name not in data["contexts"]:
                raise ValueError(f"Context '{name}' does not exist")

            ctx_data = data["contexts"][name]
            existing = self._extract_context_credential(ctx_data)
            if existing and not overwrite:
                raise ValueError(f"Context '{name}' already has a fallback credential")

            self._write_context_credential(ctx_data, username, password)

            return self._write_credentials_locked(data)

    def delete_context(self, name: str) -> bool:
        """
        Delete entire context

        Args:
            name: Context name

        Returns:
            bool: True if successful, False if not found
        """
        with self._exclusive_lock():
            data = self._load_fresh_credentials()

            if name not in data["contexts"]:
                return False

            del data["contexts"][name]

            self._write_credentials_locked(data)
            return True

    def list_contexts(self) -> List[Dict]:
        """
        List all contexts with their details

        Returns:
            list[dict]: List of contexts with name, git_include_file, credential count
        """
        data = self.decrypt_credentials()

        contexts = []
        for name, ctx_data in data["contexts"].items():
            if not isinstance(ctx_data, dict):
                continue

            git_include_file = ctx_data.get("git_include_file", "")
            credential = self._extract_context_credential(ctx_data)

            contexts.append(
                {
                    "name": name,
                    "git_include_file": git_include_file,
                    "credential_count": 1 if credential else 0,
                    "credential": credential,
                }
            )

        return sorted(contexts, key=lambda x: x["name"])

    # ============================================
    # Host Management Methods
    # ============================================

    def add_host_credential(self, hostname: str, username: str, password: str) -> bool:
        """
        Add credential to host

        Args:
            hostname: Hostname
            username: Username
            password: Password

        Returns:
            bool: True if successful
        """
        with self._exclusive_lock():
            data = self._load_fresh_credentials()

            if hostname not in data["hosts"]:
                data["hosts"][hostname] = {"credentials": []}

            for cred in data["hosts"][hostname]["credentials"]:
                if cred["username"] == username:
                    raise ValueError(
                        f"Username '{username}' already exists for host '{hostname}'"
                    )

            data["hosts"][hostname]["credentials"].append(
                {"username": username, "password": password}
            )

            return self._write_credentials_locked(data)

    def delete_host_credential(self, hostname: str, username: str) -> bool:
        """
        Delete specific credential from host

        Args:
            hostname: Hostname
            username: Username to delete

        Returns:
            bool: True if successful, False if not found
        """
        with self._exclusive_lock():
            data = self._load_fresh_credentials()

            if hostname not in data["hosts"]:
                return False

            credentials = data["hosts"][hostname]["credentials"]

            for i, cred in enumerate(credentials):
                if cred["username"] == username:
                    credentials.pop(i)

                    if not credentials:
                        del data["hosts"][hostname]

                    self._write_credentials_locked(data)
                    return True

            return False

    def delete_host_all_credentials(self, hostname: str) -> bool:
        """
        Delete all credentials for host

        Args:
            hostname: Hostname

        Returns:
            bool: True if successful, False if not found
        """
        with self._exclusive_lock():
            data = self._load_fresh_credentials()

            if hostname not in data["hosts"]:
                return False

            del data["hosts"][hostname]

            self._write_credentials_locked(data)
            return True

    def list_hosts(self) -> List[Dict]:
        """
        List all hosts with their credential counts

        Returns:
            list[dict]: List of hosts with hostname, credential count
        """
        data = self.decrypt_credentials()

        hosts = []
        for hostname, host_data in data["hosts"].items():
            # Defensive coding - handle potential missing keys
            if not isinstance(host_data, dict):
                continue

            credentials = host_data.get("credentials", [])

            hosts.append(
                {
                    "hostname": hostname,
                    "credential_count": len(credentials),
                    "credentials": credentials,
                }
            )

        return sorted(hosts, key=lambda x: x["hostname"])


if __name__ == "__main__":
    # Simple test
    cm = CredentialManager()
    print("Credential manager initialized successfully")

    contexts = cm.list_contexts()
    hosts = cm.list_hosts()
    print(f"Found {len(contexts)} contexts, {len(hosts)} hosts")
