"""Tests for SSH key generation and InMemoryKeyStore."""
import asyncio
import pytest
import sys
import os

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from jupyterhub_ssh.keys import generate_ed25519_keypair, InMemoryKeyStore


def test_generate_keypair_returns_two_strings():
    private_key, public_key = generate_ed25519_keypair()
    assert isinstance(private_key, str)
    assert isinstance(public_key, str)


def test_public_key_starts_with_ssh_ed25519():
    _, public_key = generate_ed25519_keypair()
    assert public_key.startswith("ssh-ed25519 ")


def test_private_key_is_pem():
    private_key, _ = generate_ed25519_keypair()
    assert "BEGIN" in private_key and "KEY" in private_key


def test_keypairs_are_unique():
    _, pub1 = generate_ed25519_keypair()
    _, pub2 = generate_ed25519_keypair()
    assert pub1 != pub2


@pytest.mark.anyio
async def test_in_memory_store_get_nonexistent_returns_none():
    store = InMemoryKeyStore()
    result = await store.get_public_key("alice")
    assert result is None


@pytest.mark.anyio
async def test_in_memory_store_set_and_get():
    store = InMemoryKeyStore()
    await store.set_public_key("alice", "ssh-ed25519 AAAA test")
    result = await store.get_public_key("alice")
    assert result == "ssh-ed25519 AAAA test"


@pytest.mark.anyio
async def test_get_or_create_generates_key_first_time():
    store = InMemoryKeyStore()
    private_key, public_key = await store.get_or_create_keypair("alice")
    assert private_key is not None
    assert public_key.startswith("ssh-ed25519 ")


@pytest.mark.anyio
async def test_get_or_create_returns_none_private_key_second_time():
    store = InMemoryKeyStore()
    await store.get_or_create_keypair("alice")
    private_key, public_key = await store.get_or_create_keypair("alice")
    assert private_key is None
    assert public_key.startswith("ssh-ed25519 ")


@pytest.mark.anyio
async def test_get_or_create_same_public_key_on_repeat():
    store = InMemoryKeyStore()
    _, pub1 = await store.get_or_create_keypair("alice")
    _, pub2 = await store.get_or_create_keypair("alice")
    assert pub1 == pub2


@pytest.mark.anyio
async def test_different_users_get_different_keys():
    store = InMemoryKeyStore()
    _, pub_alice = await store.get_or_create_keypair("alice")
    _, pub_bob = await store.get_or_create_keypair("bob")
    assert pub_alice != pub_bob
