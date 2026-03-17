"""JupyterLab extension: Open in VS Code button for JupyterHub."""
try:
    from ._version import __version__
except ImportError:
    import warnings
    warnings.warn("Could not load version for jhub_vscode_labext")
    __version__ = "unknown"


def _jupyter_labextension_paths():
    return [{
        "src": "labextension",
        "dest": "@groundsada/jupyterhub-vscode",
    }]
