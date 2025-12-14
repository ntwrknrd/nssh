"""nssh - SSH host and password management tools"""

# Lazy-load version to avoid 35ms importlib.metadata overhead on every import.
# importlib.metadata.version() is expensive because it scans installed packages.
# This defers the cost until something actually needs __version__.

_version_cache: str | None = None


def __getattr__(name: str) -> str:
    """Lazy attribute access for __version__."""
    if name == "__version__":
        global _version_cache
        if _version_cache is None:
            from importlib.metadata import PackageNotFoundError, version

            try:
                _version_cache = version("nssh")
            except PackageNotFoundError:
                # Package not installed, fallback for development
                _version_cache = "dev"
        return _version_cache
    raise AttributeError(f"module {__name__!r} has no attribute {name!r}")
