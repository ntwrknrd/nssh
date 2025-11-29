"""Tests for XDG migration functionality."""

from __future__ import annotations

from nssh.core.env import migrate


def test_migrate_legacy_host_index(tmp_path, monkeypatch):
    """Test migration of .nssh_host_index from ~/.ssh/ to XDG state dir."""
    # Create legacy file
    legacy_ssh = tmp_path / ".ssh"
    legacy_ssh.mkdir()
    legacy_index = legacy_ssh / ".nssh_host_index"
    legacy_index.write_text("router1|/tmp/hosts\n")

    # Set up new location
    new_state = tmp_path / ".local" / "state" / "nssh"

    # Patch the mappings and helper functions
    monkeypatch.setattr(
        migrate,
        "LEGACY_MAPPINGS",
        [(str(legacy_index), "state", "host_index")],
    )
    monkeypatch.setattr(migrate, "default_state_root", lambda: new_state)
    monkeypatch.setattr(
        migrate, "default_data_root", lambda: tmp_path / ".local" / "share" / "nssh"
    )

    # Run migration (no prompts in tests)
    migrated = migrate.migrate_legacy_files(prompt_overwrite=False)

    # Verify migration occurred
    assert len(migrated) == 1
    old, new = migrated[0]
    assert old == legacy_index
    assert new == new_state / "host_index"
    assert new.exists()
    assert new.read_text() == "router1|/tmp/hosts\n"
    assert not legacy_index.exists()


def test_migrate_legacy_credentials(tmp_path, monkeypatch):
    """Test migration of nssh_credentials.age from ~/.ssh/ to XDG data dir."""
    # Create legacy file
    legacy_ssh = tmp_path / ".ssh"
    legacy_ssh.mkdir()
    legacy_creds = legacy_ssh / "nssh_credentials.age"
    legacy_creds.write_text("encrypted-data")

    # Set up new location
    new_data = tmp_path / ".local" / "share" / "nssh"

    # Patch the mappings and helper functions
    monkeypatch.setattr(
        migrate,
        "LEGACY_MAPPINGS",
        [(str(legacy_creds), "data", "credentials.age")],
    )
    monkeypatch.setattr(
        migrate, "default_state_root", lambda: tmp_path / ".local" / "state" / "nssh"
    )
    monkeypatch.setattr(migrate, "default_data_root", lambda: new_data)

    # Run migration (no prompts in tests)
    migrated = migrate.migrate_legacy_files(prompt_overwrite=False)

    # Verify migration occurred
    assert len(migrated) == 1
    old, new = migrated[0]
    assert old == legacy_creds
    assert new == new_data / "credentials.age"
    assert new.exists()
    assert new.read_text() == "encrypted-data"
    assert not legacy_creds.exists()


def test_migrate_legacy_backups_directory(tmp_path, monkeypatch):
    """Test migration of backups directory from ~/.ssh/ to XDG data dir."""
    # Create legacy directory with files
    legacy_ssh = tmp_path / ".ssh"
    legacy_ssh.mkdir()
    legacy_backups = legacy_ssh / "backups"
    legacy_backups.mkdir()
    (legacy_backups / "config.bak.20241128").write_text("backup-content")

    # Set up new location
    new_data = tmp_path / ".local" / "share" / "nssh"

    # Patch the mappings and helper functions
    monkeypatch.setattr(
        migrate,
        "LEGACY_MAPPINGS",
        [(str(legacy_backups), "data", "backups")],
    )
    monkeypatch.setattr(
        migrate, "default_state_root", lambda: tmp_path / ".local" / "state" / "nssh"
    )
    monkeypatch.setattr(migrate, "default_data_root", lambda: new_data)

    # Run migration (no prompts in tests)
    migrated = migrate.migrate_legacy_files(prompt_overwrite=False)

    # Verify migration occurred
    assert len(migrated) == 1
    old, new = migrated[0]
    assert old == legacy_backups
    assert new == new_data / "backups"
    assert new.is_dir()
    assert (new / "config.bak.20241128").read_text() == "backup-content"
    assert not legacy_backups.exists()


def test_migrate_skips_if_new_location_exists(tmp_path, monkeypatch):
    """Test that migration doesn't overwrite existing files at new location."""
    # Create both legacy and new files
    legacy_ssh = tmp_path / ".ssh"
    legacy_ssh.mkdir()
    legacy_index = legacy_ssh / ".nssh_host_index"
    legacy_index.write_text("old-data")

    new_state = tmp_path / ".local" / "state" / "nssh"
    new_state.mkdir(parents=True)
    new_index = new_state / "host_index"
    new_index.write_text("new-data")

    # Patch the mappings and helper functions
    monkeypatch.setattr(
        migrate,
        "LEGACY_MAPPINGS",
        [(str(legacy_index), "state", "host_index")],
    )
    monkeypatch.setattr(migrate, "default_state_root", lambda: new_state)
    monkeypatch.setattr(
        migrate, "default_data_root", lambda: tmp_path / ".local" / "share" / "nssh"
    )

    # Run migration (no prompts in tests)
    migrated = migrate.migrate_legacy_files(prompt_overwrite=False)

    # Verify no migration occurred
    assert len(migrated) == 0
    assert legacy_index.exists()  # Legacy file untouched
    assert new_index.read_text() == "new-data"  # New file preserved


def test_migrate_no_legacy_files(tmp_path, monkeypatch):
    """Test migration returns empty list when no legacy files exist."""
    nonexistent = tmp_path / "nonexistent"

    # Patch the mappings and helper functions
    monkeypatch.setattr(
        migrate,
        "LEGACY_MAPPINGS",
        [(str(nonexistent), "state", "host_index")],
    )
    monkeypatch.setattr(
        migrate, "default_state_root", lambda: tmp_path / ".local" / "state" / "nssh"
    )
    monkeypatch.setattr(
        migrate, "default_data_root", lambda: tmp_path / ".local" / "share" / "nssh"
    )

    # Run migration with no legacy files
    migrated = migrate.migrate_legacy_files()

    assert migrated == []


def test_migrate_all_files(tmp_path, monkeypatch):
    """Test migration of all three file types at once."""
    # Create all legacy files
    legacy_ssh = tmp_path / ".ssh"
    legacy_ssh.mkdir()
    legacy_index = legacy_ssh / ".nssh_host_index"
    legacy_index.write_text("index-data")
    legacy_creds = legacy_ssh / "nssh_credentials.age"
    legacy_creds.write_text("creds-data")
    legacy_backups = legacy_ssh / "backups"
    legacy_backups.mkdir()
    (legacy_backups / "file.bak").write_text("backup-data")

    new_state = tmp_path / ".local" / "state" / "nssh"
    new_data = tmp_path / ".local" / "share" / "nssh"

    # Patch the mappings and helper functions
    monkeypatch.setattr(
        migrate,
        "LEGACY_MAPPINGS",
        [
            (str(legacy_index), "state", "host_index"),
            (str(legacy_creds), "data", "credentials.age"),
            (str(legacy_backups), "data", "backups"),
        ],
    )
    monkeypatch.setattr(migrate, "default_state_root", lambda: new_state)
    monkeypatch.setattr(migrate, "default_data_root", lambda: new_data)

    # Run migration (no prompts in tests)
    migrated = migrate.migrate_legacy_files(prompt_overwrite=False)

    # Verify all migrations occurred
    assert len(migrated) == 3

    # Verify new locations
    assert (new_state / "host_index").read_text() == "index-data"
    assert (new_data / "credentials.age").read_text() == "creds-data"
    assert (new_data / "backups" / "file.bak").read_text() == "backup-data"
