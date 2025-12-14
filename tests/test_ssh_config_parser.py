from __future__ import annotations

from nssh.core.ssh.config import SSHConfigParser


def test_parse_ssh_config_splits_header_and_hosts(tmp_path) -> None:
    config_file = tmp_path / "work_hosts"
    config_file.write_text(
        "Host *\n" "  User root\n" "Host router1\n" "  HostName 10.0.0.5\n"
    )

    parser = SSHConfigParser(config_file=config_file)
    header, hosts = parser.parse_ssh_config(config_file)

    assert any(line.startswith("Host *") for line in header)
    assert hosts[0][0] == "router1"
    assert "HostName 10.0.0.5" in "".join(hosts[0][1])


def test_find_host_in_files_returns_matching_include(tmp_path) -> None:
    main_config = tmp_path / "config"
    include_path = tmp_path / "work_hosts"

    include_path.write_text("Host router1\n" "  HostName 10.0.0.5\n")
    main_config.write_text(f"Include {include_path}\n")

    parser = SSHConfigParser(config_file=main_config)
    result = parser.find_host_in_files("router1", include_files=[include_path])

    assert result is not None
    file_path, host_lines = result
    assert file_path == include_path
    assert any(line.strip() == "Host router1" for line in host_lines)


def test_find_insertion_index_orders_hosts() -> None:
    parser = SSHConfigParser()
    hosts = [
        ("alpha", ["Host alpha\n"]),
        ("omega", ["Host omega\n"]),
    ]

    assert parser.find_insertion_index(hosts, "aardvark") == 0
    assert parser.find_insertion_index(hosts, "zeta") == 2
    assert parser.find_insertion_index(hosts, "delta") == 1


def test_write_ssh_config_sets_permissions(tmp_path, monkeypatch) -> None:
    target = tmp_path / "config"
    parser = SSHConfigParser()
    calls: list[str] = []

    monkeypatch.setattr(
        "nssh.core.ssh.config.set_secure_permissions",
        lambda path: calls.append(str(path)),
    )

    parser.write_ssh_config(
        target,
        ["# header\n"],
        [("router1", ["Host router1\n", "  HostName 10.0.0.5\n"])],
    )

    assert "Host router1" in target.read_text()
    assert calls == [str(target)]


def test_create_backup_writes_to_configured_directory(tmp_path, monkeypatch) -> None:
    backup_dir = tmp_path / "backups"
    monkeypatch.setattr("nssh.core.ssh.config.NSSH_BACKUP_DIR", backup_dir)

    config = tmp_path / "config"
    config.write_text("Host router1\n")

    parser = SSHConfigParser(config_file=config)
    backup_path = parser.create_backup(config)

    assert backup_path.parent == backup_dir
    assert backup_path.read_text() == config.read_text()


def test_host_exists_and_surrounding_hosts(tmp_path) -> None:
    parser = SSHConfigParser()
    hosts = [
        ("alpha", ["Host alpha\n"]),
        ("delta", ["Host delta\n"]),
        ("omega", ["Host omega\n"]),
    ]

    assert parser.host_exists(tmp_path / "config", "delta", hosts) is True
    before, after = parser.get_surrounding_hosts(hosts, 1, context=2)
    assert before == ["alpha"]
    assert after == ["delta", "omega"]


def test_host_exists_supports_aliases(tmp_path) -> None:
    parser = SSHConfigParser()
    hosts = [("router", ["Host router router-admin\n"])]

    assert parser.host_exists(tmp_path / "config", "router-admin", hosts) is True


def test_rebuild_index_collects_hosts_from_all_files(tmp_path, monkeypatch) -> None:
    ssh_dir = tmp_path / ".ssh"
    ssh_dir.mkdir()
    config = ssh_dir / "config"
    include = ssh_dir / "work_hosts"

    config.write_text(f"Host main\n  HostName main.example\nInclude {include}\n")
    include.write_text("Host backup\n  HostName backup.example\n")

    # Isolate index to temp path to avoid polluting user's real index
    monkeypatch.setenv("NSSH_HOST_INDEX", str(tmp_path / "host_index"))
    monkeypatch.setattr("nssh.core.ssh.config.Path.home", lambda: tmp_path)
    calls: list[str] = []
    monkeypatch.setattr(
        "nssh.core.ssh.config.set_secure_permissions",
        lambda path: calls.append(str(path)),
    )

    parser = SSHConfigParser(config_file=config)
    index_path = parser.rebuild_index()

    contents = index_path.read_text().splitlines()
    assert f"main|{config}" in contents
    assert f"backup|{include}" in contents
    assert calls[-1] == str(index_path)


def test_rebuild_index_emits_all_aliases(tmp_path, monkeypatch) -> None:
    ssh_dir = tmp_path / ".ssh"
    ssh_dir.mkdir()
    config = ssh_dir / "config"
    config.write_text("Host edge switch-edge\n  HostName edge\n")

    # Isolate index to temp path to avoid polluting user's real index
    monkeypatch.setenv("NSSH_HOST_INDEX", str(tmp_path / "host_index"))
    monkeypatch.setattr("nssh.core.ssh.config.Path.home", lambda: tmp_path)
    monkeypatch.setattr(
        "nssh.core.ssh.config.set_secure_permissions", lambda _path: None
    )

    parser = SSHConfigParser(config_file=config)
    index_path = parser.rebuild_index()
    contents = index_path.read_text().splitlines()
    assert any(line.startswith("edge|") for line in contents)
    assert any(line.startswith("switch-edge|") for line in contents)


def test_find_include_files_handles_globs_and_case(tmp_path) -> None:
    config = tmp_path / "config"
    include_dir = tmp_path / "config.d"
    include_dir.mkdir()
    file_a = include_dir / "a.conf"
    file_b = include_dir / "b.conf"
    file_a.write_text("Host a\n  HostName a\n")
    file_b.write_text("Host b\n  HostName b\n")
    extra_file = tmp_path / "EXTRA.conf"
    extra_file.write_text("Host extra\n  HostName extra\n")

    config.write_text("include config.d/*.conf\nInclude EXTRA.conf\n")

    parser = SSHConfigParser(config_file=config)
    includes = parser.find_include_files()

    assert file_a.resolve() in includes
    assert file_b.resolve() in includes
    assert extra_file.resolve() in includes


def test_find_host_in_files_matches_aliases(tmp_path) -> None:
    main_config = tmp_path / "config"
    include_path = tmp_path / "work_hosts"

    include_path.write_text("Host router1 router-backup\n  HostName 10.0.0.5\n")
    main_config.write_text(f"Include {include_path}\n")

    parser = SSHConfigParser(config_file=main_config)
    result = parser.find_host_in_files("router-backup", include_files=[include_path])

    assert result is not None
    file_path, _ = result
    assert file_path == include_path
