"""Tests for the spawner pre_spawn_hook."""
import asyncio
import pytest
import sys
import os

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from jupyterhub_ssh.keys import InMemoryKeyStore
from jupyterhub_ssh.hooks import make_ssh_pre_spawn_hook


class MockUser:
    def __init__(self, name):
        self.name = name


class MockSpawner:
    """Minimal mock of KubeSpawner for testing the hook."""

    def __init__(self, username):
        self.user = MockUser(username)
        self.extra_containers = []
        self.extra_volumes = []
        self.extra_volume_mounts = []
        self.environment = {}
        self._jhub_ssh_private_key = None


@pytest.mark.anyio
async def test_hook_adds_ssh_sidecar_container():
    store = InMemoryKeyStore()
    hook = make_ssh_pre_spawn_hook(key_store=store, sidecar_image="test-image:latest")
    spawner = MockSpawner("alice")

    await hook(spawner)

    assert len(spawner.extra_containers) == 1
    assert spawner.extra_containers[0]["name"] == "ssh-sidecar"
    assert spawner.extra_containers[0]["image"] == "test-image:latest"


@pytest.mark.anyio
async def test_hook_sets_authorized_key_in_sidecar_env():
    store = InMemoryKeyStore()
    hook = make_ssh_pre_spawn_hook(key_store=store)
    spawner = MockSpawner("alice")

    await hook(spawner)

    sidecar = spawner.extra_containers[0]
    env_map = {e["name"]: e["value"] for e in sidecar["env"]}
    assert "JHUB_SSH_AUTHORIZED_KEY" in env_map
    assert env_map["JHUB_SSH_AUTHORIZED_KEY"].startswith("ssh-ed25519 ")


@pytest.mark.anyio
async def test_hook_stores_private_key_on_spawner():
    store = InMemoryKeyStore()
    hook = make_ssh_pre_spawn_hook(key_store=store)
    spawner = MockSpawner("alice")

    await hook(spawner)

    assert spawner._jhub_ssh_private_key is not None
    assert "BEGIN" in spawner._jhub_ssh_private_key


@pytest.mark.anyio
async def test_hook_second_spawn_no_new_private_key():
    store = InMemoryKeyStore()
    hook = make_ssh_pre_spawn_hook(key_store=store)

    spawner1 = MockSpawner("alice")
    await hook(spawner1)
    assert spawner1._jhub_ssh_private_key is not None

    spawner2 = MockSpawner("alice")
    await hook(spawner2)
    assert spawner2._jhub_ssh_private_key is None  # key already existed


@pytest.mark.anyio
async def test_hook_adds_shared_home_volume():
    store = InMemoryKeyStore()
    hook = make_ssh_pre_spawn_hook(key_store=store)
    spawner = MockSpawner("alice")

    await hook(spawner)

    vol_names = [v["name"] for v in spawner.extra_volumes]
    assert "home" in vol_names


@pytest.mark.anyio
async def test_hook_skips_volume_if_pvc_already_exists():
    store = InMemoryKeyStore()
    hook = make_ssh_pre_spawn_hook(key_store=store, home_pvc_volume_name="claim-alice")
    spawner = MockSpawner("alice")

    await hook(spawner)

    # No extra volume should be added since the PVC already provides home
    assert spawner.extra_volumes == []


@pytest.mark.anyio
async def test_hook_does_not_double_add_sidecar():
    store = InMemoryKeyStore()
    hook = make_ssh_pre_spawn_hook(key_store=store)
    spawner = MockSpawner("alice")

    await hook(spawner)
    await hook(spawner)  # second call (e.g., named server)

    ssh_sidecars = [c for c in spawner.extra_containers if c["name"] == "ssh-sidecar"]
    assert len(ssh_sidecars) == 1


@pytest.mark.anyio
async def test_hook_sets_ssh_port_env_var():
    store = InMemoryKeyStore()
    hook = make_ssh_pre_spawn_hook(key_store=store, ssh_port=2222)
    spawner = MockSpawner("alice")

    await hook(spawner)

    assert spawner.environment["JHUB_SSH_PORT"] == "2222"


@pytest.mark.anyio
async def test_hook_custom_ssh_port_in_sidecar():
    store = InMemoryKeyStore()
    hook = make_ssh_pre_spawn_hook(key_store=store, ssh_port=2200)
    spawner = MockSpawner("alice")

    await hook(spawner)

    sidecar = spawner.extra_containers[0]
    assert sidecar["ports"][0]["containerPort"] == 2200
