"""Unit tests for sidecar_spec.py"""
import sys
import os

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from sidecar_spec import ssh_sidecar_container, ssh_shared_volume, ssh_shared_volume_mount


def test_sidecar_container_has_required_fields():
    spec = ssh_sidecar_container(authorized_key="ssh-ed25519 AAAA test@host")
    assert spec["name"] == "ssh-sidecar"
    assert spec["ports"][0]["containerPort"] == 2222
    assert spec["ports"][0]["protocol"] == "TCP"


def test_sidecar_env_contains_authorized_key():
    key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA test@laptop"
    spec = ssh_sidecar_container(authorized_key=key)
    env_map = {e["name"]: e["value"] for e in spec["env"]}
    assert env_map["JHUB_SSH_AUTHORIZED_KEY"] == key


def test_sidecar_no_key_produces_empty_env():
    spec = ssh_sidecar_container(authorized_key=None)
    assert spec["env"] == []


def test_sidecar_mounts_home():
    spec = ssh_sidecar_container(authorized_key=None)
    mounts = {m["name"]: m["mountPath"] for m in spec["volumeMounts"]}
    assert mounts["home"] == "/home/jovyan"


def test_sidecar_custom_port():
    spec = ssh_sidecar_container(authorized_key=None, ssh_port=2200)
    assert spec["ports"][0]["containerPort"] == 2200
    assert spec["readinessProbe"]["tcpSocket"]["port"] == 2200
    assert spec["livenessProbe"]["tcpSocket"]["port"] == 2200


def test_shared_volume_emptydir_when_no_pvc():
    vol = ssh_shared_volume(existing_home_volume_name=None)
    assert vol is not None
    assert vol["name"] == "home"
    assert "emptyDir" in vol


def test_shared_volume_none_when_pvc_exists():
    vol = ssh_shared_volume(existing_home_volume_name="claim-jovyan")
    assert vol is None


def test_shared_volume_mount():
    mount = ssh_shared_volume_mount()
    assert mount["name"] == "home"
    assert mount["mountPath"] == "/home/jovyan"


def test_resource_limits_present():
    spec = ssh_sidecar_container(authorized_key=None)
    assert "limits" in spec["resources"]
    assert "requests" in spec["resources"]
    assert spec["resources"]["limits"]["memory"] == "64Mi"


def test_security_context_drops_all_except_setuid_setgid():
    spec = ssh_sidecar_container(authorized_key=None)
    caps = spec["securityContext"]["capabilities"]
    assert "SETUID" in caps["add"]
    assert "SETGID" in caps["add"]
    assert caps["drop"] == ["ALL"]
