"""Tests for config discovery, safe naming, and credential hints.

Standalone:  python console/backend/tests/test_configstore.py
"""

from __future__ import annotations

import contextlib
import os
import pathlib
import sys
import tempfile
import typing as t

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from console.backend import configstore, paths  # noqa: E402

_STATE_ENV = "DREADGOAD_CONSOLE_STATE_ROOT"


@contextlib.contextmanager
def isolated_state() -> t.Iterator[pathlib.Path]:
    """Point the state root (and so configs_root) at a throwaway directory."""
    saved = os.environ.get(_STATE_ENV)
    with tempfile.TemporaryDirectory() as d:
        os.environ[_STATE_ENV] = d
        try:
            yield pathlib.Path(d)
        finally:
            if saved is None:
                os.environ.pop(_STATE_ENV, None)
            else:
                os.environ[_STATE_ENV] = saved


def test_slug_for_reduces_to_a_safe_stem() -> None:
    assert configstore.slug_for("Azure Lab #1") == "azure-lab-1"
    assert configstore.slug_for("  RedTeam  ") == "redteam"
    # Long names are bounded, not rejected.
    assert len(configstore.slug_for("x" * 200)) == 48
    print("PASS test_slug_for_reduces_to_a_safe_stem")


def test_slug_for_defuses_traversal_and_hidden_files() -> None:
    """The name reaches this from the browser, so it must not build a path."""
    # ".." on its own reduces to nothing and is rejected outright; it is covered
    # by test_slug_for_rejects_names_with_nothing_usable.
    for hostile in (
        "../../../etc/cron.d/evil",
        "/etc/passwd",
        ".ssh/authorized_keys",
        "a/../../b",
    ):
        slug = configstore.slug_for(hostile)
        assert "/" not in slug, (hostile, slug)
        assert not slug.startswith("."), (hostile, slug)
        assert ".." not in slug, (hostile, slug)
    print("PASS test_slug_for_defuses_traversal_and_hidden_files")


def test_slug_for_rejects_names_with_nothing_usable() -> None:
    for empty in ("", "   ", "///", "..", "!!!"):
        try:
            configstore.slug_for(empty)
            raise AssertionError(f"expected ValueError for {empty!r}")
        except ValueError:
            pass
    print("PASS test_slug_for_rejects_names_with_nothing_usable")


def test_path_for_stays_inside_the_configs_root() -> None:
    with isolated_state():
        root = paths.configs_root().resolve()
        for name in ("normal", "../../escape", "/absolute/thing"):
            path = configstore.path_for(name)
            assert path.parent == root, (name, path, root)
            assert path.suffix == ".yaml", path
    print("PASS test_path_for_stays_inside_the_configs_root")


def _write(path: pathlib.Path, body: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(body)


def test_known_configs_unions_sources_and_dedupes() -> None:
    with isolated_state():
        # Resolved: known_configs reports resolved paths so they compare equal to
        # session anchors, which are always resolved.
        root = paths.configs_root().resolve()
        _write(
            root / "azure-lab.yaml",
            "provider: azure\nregion: eastus\nenvironments:\n  rt:\n    variant: true\n",
        )
        with tempfile.TemporaryDirectory() as elsewhere:
            external = pathlib.Path(elsewhere) / "dreadgoad.yaml"
            _write(external, "provider: aws\nenvironments:\n  prod: {}\n")

            configs = configstore.known_configs(
                # The managed one passed again as an anchor: it must not appear
                # twice just because a session already points at it.
                [str(root / "azure-lab.yaml"), str(external)]
            )

        by_path = {c["path"]: c for c in configs}
        assert len(by_path) == len(configs), f"duplicate entries: {configs}"

        managed = by_path[str(root / "azure-lab.yaml")]
        assert managed["source"] == "managed", managed
        assert managed["provider"] == "azure", managed
        assert managed["region"] == "eastus", managed
        assert managed["environments"] == ["rt"], managed
        assert managed["error"] is None, managed

        ext = by_path[str(external.resolve())]
        assert ext["source"] == "session", "a config only reachable via an anchor"
        assert ext["provider"] == "aws", ext

        # The contract the frontend matches config-to-session on.
        for c in configs:
            assert c["path"] == str(pathlib.Path(c["path"]).resolve()), (
                f"unresolved path would never equal a session anchor: {c['path']}"
            )

        # The repo-root default is always offered, and always first.
        assert configs[0]["source"] == "default", configs[0]
        assert configs[0]["path"] == configstore.default_config_path()
    print("PASS test_known_configs_unions_sources_and_dedupes")


def test_known_configs_reports_broken_configs_instead_of_dropping_them() -> None:
    """The broken one is the one being looked for; hiding it is the worst answer."""
    with isolated_state():
        root = paths.configs_root().resolve()
        _write(root / "bad.yaml", "provider: aws\nenvironments:\n  - this is a list\n")
        _write(root / "unparseable.yaml", "key: [unclosed\n")

        configs = {c["name"]: c for c in configstore.known_configs()}

        assert "bad.yaml" in configs, "a config with the wrong shape must still list"
        assert configs["bad.yaml"]["error"], configs["bad.yaml"]

        assert "unparseable.yaml" in configs, "invalid YAML must still list"
        assert "not valid YAML" in configs["unparseable.yaml"]["error"], configs[
            "unparseable.yaml"
        ]

        # A config whose file vanished between sessions is reported, not dropped.
        gone = str(root / "deleted-by-hand.yaml")
        listed = {c["path"]: c for c in configstore.known_configs([gone])}
        assert gone in listed, "an anchor pointing at a missing file must still list"
        assert listed[gone]["error"] == "file no longer exists", listed[gone]
    print("PASS test_known_configs_reports_broken_configs_instead_of_dropping_them")


def test_known_configs_ignores_non_yaml_in_the_configs_dir() -> None:
    with isolated_state():
        root = paths.configs_root().resolve()
        _write(root / "notes.txt", "not a config")
        _write(root / "azure-lab.yaml.bak.1", "provider: aws\n")
        _write(root / "real.yml", "provider: aws\nenvironments:\n  a: {}\n")

        names = {c["name"] for c in configstore.known_configs()}
        assert "notes.txt" not in names, "only .yaml/.yml are configs"
        assert "azure-lab.yaml.bak.1" not in names, (
            "backups written by write_new_env must not be offered as configs"
        )
        assert "real.yml" in names, ".yml counts too"
    print("PASS test_known_configs_ignores_non_yaml_in_the_configs_dir")


def test_credential_hint_is_advisory_both_ways() -> None:
    saved = {
        k: os.environ.pop(k, None)
        for k in (
            "AWS_PROFILE",
            "AWS_ACCESS_KEY_ID",
            "AWS_ROLE_ARN",
            "AWS_WEB_IDENTITY_TOKEN_FILE",
            "AWS_CONTAINER_CREDENTIALS_RELATIVE_URI",
        )
    }
    try:
        os.environ["AWS_PROFILE"] = "lab"
        assert configstore.credential_hint("aws") is None, "env var counts as present"

        os.environ.pop("AWS_PROFILE")
        # HOME is redirected so ~/.aws cannot exist. Branching on whether the
        # developer happens to have one made this assert nothing: on a machine
        # with ~/.aws the hint is None and the test only re-checked that ~/.aws
        # exists, which is true no matter what credential_hint does. Replacing
        # the whole function body with `return None` passed.
        home = os.environ.get("HOME")
        with tempfile.TemporaryDirectory() as fake_home:
            os.environ["HOME"] = fake_home
            try:
                hint = configstore.credential_hint("aws")
                assert hint, "no credentials anywhere should produce a hint"
                assert "still fine" in hint, f"must not read as blocking: {hint}"
                assert "doctor" in hint, f"should point somewhere better: {hint}"

                # A well-known file is enough on its own, with no env var set.
                aws_dir = pathlib.Path(fake_home) / ".aws"
                aws_dir.mkdir()
                (aws_dir / "credentials").write_text("[default]\n")
                assert configstore.credential_hint("aws") is None, (
                    "~/.aws/credentials should count as credentials being present"
                )

                # A provider with no known credential sources is never warned about.
                assert configstore.credential_hint("proxmox") is None
            finally:
                if home is None:
                    os.environ.pop("HOME", None)
                else:
                    os.environ["HOME"] = home
    finally:
        for k, v in saved.items():
            os.environ.pop(k, None)
            if v is not None:
                os.environ[k] = v
    print("PASS test_credential_hint_is_advisory_both_ways")


if __name__ == "__main__":
    test_slug_for_reduces_to_a_safe_stem()
    test_slug_for_defuses_traversal_and_hidden_files()
    test_slug_for_rejects_names_with_nothing_usable()
    test_path_for_stays_inside_the_configs_root()
    test_known_configs_unions_sources_and_dedupes()
    test_known_configs_reports_broken_configs_instead_of_dropping_them()
    test_known_configs_ignores_non_yaml_in_the_configs_dir()
    test_credential_hint_is_advisory_both_ways()
    print("ALL PASS")
