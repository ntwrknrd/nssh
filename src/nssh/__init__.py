"""nssh - SSH host and password management tools"""

from importlib.metadata import version, PackageNotFoundError

try:
    __version__ = version("nssh")
except PackageNotFoundError:
    # Package not installed, fallback for development
    __version__ = "dev"
