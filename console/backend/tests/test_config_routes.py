"""Tests for the /api/environments config-path diagnostics.

Every case runs against a real temp directory rather than a patched os module:
the whole point of these messages is to describe the filesystem accurately, and
a mock would let a wrong description pass.

Standalone:  python console/backend/tests/test_config_routes.py
"""

from __future__ import annotations

import os
import pathlib
import stat
import sys
import tempfile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from console.backend.config_routes import _config_path_problem  # noqa: E402

_YAML = "provider: aws\nregion: us-west-2\nenvironments:\n  dev: {}\n"


def test_typo_in_filename_names_the_directory() -> None:
    """The reported case: one wrong character in the filename.

    The old message was "[Errno 2] No such file or directory: '/path'". This
    must instead separate "the filename is wrong" from "the directory is
    wrong", because they send you to different places to look.
    """
    with tempfile.TemporaryDirectory() as d:
        good = pathlib.Path(d) / "dreadgoad-dreadindex2.yaml"
        good.write_text(_YAML)
        typo = str(pathlib.Path(d) / "dreadgoad-dreadindex-2.yaml")

        problem = _config_path_problem(typo)
        assert problem is not None
        assert "No such file" in problem, problem
        assert typo in problem, problem
        assert d in problem and "exists" in problem, problem
        assert "check the filename" in problem, problem
        # None of the interpreter's phrasing survives.
        assert "Errno" not in problem, problem
        print("PASS test_typo_in_filename_names_the_directory")


def test_wrong_directory_says_so() -> None:
    with tempfile.TemporaryDirectory() as d:
        missing = str(pathlib.Path(d) / "nope" / "dreadgoad.yaml")
        problem = _config_path_problem(missing)
        assert problem is not None
        assert "does not exist" in problem, problem
        assert "check the filename" not in problem, problem
        print("PASS test_wrong_directory_says_so")


def test_directory_instead_of_file() -> None:
    # Tab-completing a path stops at the directory more often than not.
    with tempfile.TemporaryDirectory() as d:
        problem = _config_path_problem(d)
        assert problem is not None
        assert "is a directory" in problem, problem
        assert "dreadgoad.yaml" in problem, problem
        print("PASS test_directory_instead_of_file")


def test_quoted_path_is_diagnosed() -> None:
    # A path copied out of a shell or YAML keeps its quotes, and the resulting
    # "not found" names a path that looks perfectly correct on screen.
    with tempfile.TemporaryDirectory() as d:
        cfg = pathlib.Path(d) / "dreadgoad.yaml"
        cfg.write_text(_YAML)
        for quote in ("'", '"'):
            problem = _config_path_problem(f"{quote}{cfg}{quote}")
            assert problem is not None
            assert "quotes" in problem, problem
            assert str(cfg) in problem, problem
        print("PASS test_quoted_path_is_diagnosed")


def test_relative_path_is_rejected_clearly() -> None:
    problem = _config_path_problem("dreadgoad.yaml")
    assert problem is not None
    assert "must be absolute" in problem, problem
    print("PASS test_relative_path_is_rejected_clearly")


def test_empty_path() -> None:
    for value in ("", "   "):
        problem = _config_path_problem(value)
        assert problem == "Config path is required.", problem
    print("PASS test_empty_path")


def test_unreadable_file() -> None:
    if os.geteuid() == 0:
        print("SKIP test_unreadable_file (running as root: mode bits do not apply)")
        return
    with tempfile.TemporaryDirectory() as d:
        cfg = pathlib.Path(d) / "dreadgoad.yaml"
        cfg.write_text(_YAML)
        cfg.chmod(0o000)
        try:
            problem = _config_path_problem(str(cfg))
            assert problem is not None
            assert "not readable" in problem, problem
        finally:
            cfg.chmod(stat.S_IRUSR | stat.S_IWUSR)
        print("PASS test_unreadable_file")


def test_good_path_has_no_problem() -> None:
    # The check gates every successful load, so a false positive here would
    # make the console unable to open a perfectly valid config.
    with tempfile.TemporaryDirectory() as d:
        cfg = pathlib.Path(d) / "dreadgoad.yaml"
        cfg.write_text(_YAML)
        assert _config_path_problem(str(cfg)) is None
        # Surrounding whitespace is normal from a paste and must not fail.
        assert _config_path_problem(f"  {cfg}  ") is None
        print("PASS test_good_path_has_no_problem")


def test_tilde_is_refused_with_the_expansion_offered() -> None:
    """A ~ path must be refused here even though the file exists.

    Accepting it would list the environments and then fail on CREATE: the
    anchor is stored verbatim and handed to open() and the Go CLI's --config,
    neither of which expands ~. That trades this endpoint's error for the same
    error one step later, where there is more to undo. Verified end to end —
    /api/environments returned 200 for a ~ path while derive_snapshot on the
    identical string raised FileNotFoundError.
    """
    home = pathlib.Path(os.path.expanduser("~"))
    with tempfile.NamedTemporaryFile(suffix=".yaml", dir=home) as f:
        tilde = "~/" + os.path.basename(f.name)
        problem = _config_path_problem(tilde)
        assert problem is not None, "a ~ path must not be accepted"
        assert "full path" in problem, problem
        # The expansion is offered so it can be pasted back into the field.
        assert str(home / os.path.basename(f.name)) in problem, problem
        print("PASS test_tilde_is_refused_with_the_expansion_offered")


def test_validation_agrees_with_what_creation_will_open() -> None:
    """Anything this endpoint accepts must be openable by session creation.

    The property that the tilde bug violated, asserted directly rather than
    through either message.
    """
    with tempfile.TemporaryDirectory() as d:
        cfg = pathlib.Path(d) / "dreadgoad.yaml"
        cfg.write_text(_YAML)
        home_rel = "~/" + os.path.basename(str(cfg))

        for candidate in (str(cfg), f"  {cfg}  ", home_rel, d, "relative.yaml", ""):
            accepted = _config_path_problem(candidate) is None
            try:
                with open(candidate.strip()) as fh:
                    fh.read()
                openable = True
            except OSError:
                openable = False
            assert not (accepted and not openable), (
                f"{candidate!r} passes validation but cannot be opened"
            )
        print("PASS test_validation_agrees_with_what_creation_will_open")


def main() -> None:
    test_typo_in_filename_names_the_directory()
    test_wrong_directory_says_so()
    test_directory_instead_of_file()
    test_quoted_path_is_diagnosed()
    test_relative_path_is_rejected_clearly()
    test_empty_path()
    test_unreadable_file()
    test_good_path_has_no_problem()
    test_tilde_is_refused_with_the_expansion_offered()
    test_validation_agrees_with_what_creation_will_open()
    print("ALL PASS")


if __name__ == "__main__":
    main()
