from setuptools import setup, find_packages

setup(
    name="jupyterhub-ssh",
    version="0.1.0",
    description="SSH access and VS Code integration for JupyterHub",
    packages=find_packages(),
    python_requires=">=3.9",
    install_requires=[
        "jupyterhub>=4.0",
        "kubernetes-asyncio>=24.0",
    ],
    extras_require={
        "crypto": ["cryptography>=41.0"],
        "dev": ["pytest>=7", "pytest-asyncio>=0.23"],
    },
    entry_points={
        "jupyterhub.spawners": [],  # not a spawner, but uses the hook API
    },
)
