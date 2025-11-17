"""Helper process and fallback snapshots for nssh timing instrumentation."""

from __future__ import annotations

import argparse
import time
from pathlib import Path
from typing import Iterable, TextIO


def _timestamp_ms() -> int:
    return int(time.time() * 1000)


def _snapshot_response(command: str) -> str:
    cmd = command.strip().split()
    if not cmd:
        return ""
    now_ns = time.perf_counter_ns()
    timestamp_ms = _timestamp_ms()
    if cmd[0] == "START":
        return f"{timestamp_ms} {now_ns}"
    if cmd[0] == "END" and len(cmd) > 1:
        try:
            start_ns = int(cmd[1])
        except ValueError:
            start_ns = now_ns
        duration = (now_ns - start_ns) / 1_000_000
        if duration < 0:
            duration = 0.0
        return f"{timestamp_ms} {now_ns} {duration:.6f}"
    return ""


def fallback_snapshot(start_ns_arg: str | None) -> str:
    if not start_ns_arg or start_ns_arg in {"", "None"}:
        start_ns = None
    else:
        try:
            start_ns = int(start_ns_arg)
        except ValueError:
            start_ns = None
    now_ns = time.perf_counter_ns()
    timestamp_ms = _timestamp_ms()
    if start_ns is None:
        return f"{timestamp_ms} {now_ns}"
    duration_ms = (now_ns - start_ns) / 1_000_000
    if duration_ms < 0:
        duration_ms = 0.0
    return f"{timestamp_ms} {now_ns} {duration_ms:.6f}"


def _process_stream(reader: Iterable[str], writer: TextIO) -> None:
    for raw in reader:
        line = raw.strip()
        if line == "STOP":
            break
        out = _snapshot_response(line)
        if out:
            writer.write(out + "\n")
            writer.flush()


def helper_loop(fifo_in: Path, fifo_out: Path) -> None:
    with fifo_in.open("r") as reader, fifo_out.open("w") as writer:
        _process_stream(reader, writer)


def _build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="nssh-timer", description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)

    fallback_cmd = sub.add_parser(
        "fallback", help="Emit single snapshot for inline fallback"
    )
    fallback_cmd.add_argument("start_ns", nargs="?", default="")

    helper_cmd = sub.add_parser("helper", help="Run FIFO helper loop")
    helper_cmd.add_argument("fifo_in", type=Path)
    helper_cmd.add_argument("fifo_out", type=Path)

    return parser


def _run(argv: list[str] | None = None) -> int:
    parser = _build_parser()
    args = parser.parse_args(argv)

    if args.command == "fallback":  # type: ignore[attr-defined]
        print(fallback_snapshot(args.start_ns))
        return 0

    if args.command == "helper":  # type: ignore[attr-defined]
        helper_loop(args.fifo_in, args.fifo_out)
        return 0

    parser.error("unknown command")
    return 1


def main() -> None:
    raise SystemExit(_run())


if __name__ == "__main__":  # pragma: no cover
    main()
