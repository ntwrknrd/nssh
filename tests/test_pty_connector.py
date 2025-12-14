from __future__ import annotations

import fcntl
import os
import threading
import time

import io

from nssh.core.connector.pty import PtyConnector


class PipeStdout:
    """Minimal stdout replacement that writes bytes to a pipe fd."""

    def __init__(self, fd: int):
        self._fd = fd
        self.buffer = self  # Mimic TextIOWrapper.buffer attribute

    def fileno(self) -> int:
        return self._fd

    def write(self, data):  # pragma: no cover - fallback path
        if isinstance(data, str):
            data = data.encode()
        os.write(self._fd, data)
        return len(data)

    def flush(self):  # pragma: no cover - fallback path
        return None


def _fill_pipe(fd: int) -> None:
    chunk = b"x" * 65536
    while True:
        try:
            os.write(fd, chunk)
        except BlockingIOError:
            return


def test_pty_connector_handles_blocking_stdout():
    r_fd, w_fd = os.pipe()
    try:
        flags = fcntl.fcntl(w_fd, fcntl.F_GETFL)
        fcntl.fcntl(w_fd, fcntl.F_SETFL, flags | os.O_NONBLOCK)

        _fill_pipe(w_fd)

        collected = bytearray()

        def _drain_reader():
            time.sleep(0.05)
            while True:
                chunk = os.read(r_fd, 4096)
                if not chunk:
                    break
                collected.extend(chunk)

        reader = threading.Thread(target=_drain_reader)
        reader.start()

        stdout = PipeStdout(w_fd)
        connector = PtyConnector(
            hostname="testhost",
            username=None,
            password=None,
            stdout=stdout,
        )

        payload = b"blocking-write-payload"
        connector._write_stdout(payload)

        os.close(w_fd)
        reader.join(timeout=1)
        assert not reader.is_alive(), "reader thread failed to drain pipe"
        assert collected.endswith(payload)
    finally:
        for fd in (r_fd, w_fd):
            try:
                os.close(fd)
            except OSError:
                pass


def test_pty_connector_writes_into_custom_bytes_buffer():
    buffer = io.BytesIO()
    connector = PtyConnector(
        hostname="dummy",
        username=None,
        password=None,
        stdout=buffer,
    )

    payload = b"hello-bytes"
    connector._write_stdout(payload)
    assert buffer.getvalue() == payload
