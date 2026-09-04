#!/usr/bin/env python3
"""Idempotently seed Gitea collaboration data and a successful Actions build."""

from __future__ import annotations

import base64
import http.cookiejar
import json
import os
import pathlib
import re
import shutil
import subprocess
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from typing import Any


GITEA_URL = "http://127.0.0.1:3000"
REPOSITORY = "rangeadmin/orchid-control-plane"
ADMIN_AUTH = ("rangeadmin", "ScopeGitea2026!")
LDAP_AUTH = {
    "alice": ("alice", "ScopeFiles2026!"),
    "bob": ("bob", "ScopeFiles2026!"),
}
SOURCE = pathlib.Path("/opt/scope-range/development/seed/orchid-control-plane")
SEED_TAG = "v0.1.0"
SEED_VERSION = "scope-seed-v1"


def authorization(auth: tuple[str, str]) -> str:
    """Return an HTTP Basic authorization value."""
    token = base64.b64encode(f"{auth[0]}:{auth[1]}".encode()).decode()
    return f"Basic {token}"


def api(
    path: str,
    *,
    method: str = "GET",
    body: object | None = None,
    auth: tuple[str, str] = ADMIN_AUTH,
    allowed: tuple[int, ...] = (200,),
) -> Any:
    """Call the local Gitea API and decode a JSON response when present."""
    data = None if body is None else json.dumps(body).encode()
    request = urllib.request.Request(
        f"{GITEA_URL}/api/v1{path}",
        data=data,
        method=method,
        headers={
            "Accept": "application/json",
            "Authorization": authorization(auth),
            "Content-Type": "application/json",
        },
    )
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            status = response.status
            payload = response.read()
    except urllib.error.HTTPError as exc:
        if exc.code not in allowed:
            detail = exc.read().decode(errors="replace")
            raise RuntimeError(
                f"Gitea {method} {path} failed: {exc.code} {detail}"
            ) from exc
        status = exc.code
        payload = exc.read()
    if status not in allowed:
        raise RuntimeError(f"Gitea {method} {path} returned {status}")
    if status == 404:
        return None
    return json.loads(payload) if payload else None


def ensure_ldap_login(username: str, password: str) -> None:
    """Provision a Gitea account through its configured LDAP login source."""
    try:
        api("/user", auth=(username, password))
        return
    except RuntimeError:
        pass

    jar = http.cookiejar.CookieJar()
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
    with opener.open(f"{GITEA_URL}/user/login", timeout=30) as response:
        page = response.read().decode()
    match = re.search(r'name="_csrf"\s+value="([^"]+)"', page)
    if match is None:
        raise RuntimeError("Gitea login page did not contain a CSRF token")
    data = urllib.parse.urlencode(
        {"_csrf": match.group(1), "user_name": username, "password": password}
    ).encode()
    request = urllib.request.Request(
        f"{GITEA_URL}/user/login",
        data=data,
        headers={
            "Content-Type": "application/x-www-form-urlencoded",
            "Referer": f"{GITEA_URL}/user/login",
        },
    )
    opener.open(request, timeout=30).read()
    api("/user", auth=(username, password))
    print(f"CHANGED provisioned LDAP-backed Gitea user {username}")


def git_command(
    worktree: pathlib.Path,
    *args: str,
    check: bool = True,
    env: dict[str, str] | None = None,
) -> subprocess.CompletedProcess[str]:
    """Run Git with an authorization header and deterministic identity."""
    command = [
        "git",
        "-c",
        f"http.extraHeader=Authorization: {authorization(ADMIN_AUTH)}",
        *args,
    ]
    return subprocess.run(
        command,
        cwd=worktree,
        check=check,
        capture_output=True,
        text=True,
        env=env,
    )


def commit_environment(name: str, email: str, timestamp: str) -> dict[str, str]:
    """Build a deterministic Git author and committer environment."""
    environment = os.environ.copy()
    environment.update(
        {
            "GIT_AUTHOR_NAME": name,
            "GIT_AUTHOR_EMAIL": email,
            "GIT_COMMITTER_NAME": name,
            "GIT_COMMITTER_EMAIL": email,
            "GIT_AUTHOR_DATE": timestamp,
            "GIT_COMMITTER_DATE": timestamp,
        }
    )
    return environment


def copy_seed(worktree: pathlib.Path, relative: str) -> None:
    """Copy one declarative seed path into a temporary Git worktree."""
    source = SOURCE / relative
    destination = worktree / relative
    destination.parent.mkdir(parents=True, exist_ok=True)
    if source.is_dir():
        shutil.copytree(source, destination, dirs_exist_ok=True)
    else:
        shutil.copy2(source, destination)


def ensure_repository_history() -> str:
    """Create the deterministic main and feature history once per seed version."""
    remote = f"{GITEA_URL}/{REPOSITORY}.git"
    with tempfile.TemporaryDirectory(prefix="scope-gitea-seed-") as directory:
        worktree = pathlib.Path(directory)
        remote_tag = git_command(
            worktree,
            "ls-remote",
            "--exit-code",
            "--tags",
            remote,
            f"refs/tags/{SEED_TAG}^{{}}",
            check=False,
        )
        if remote_tag.returncode == 0:
            return remote_tag.stdout.split()[0]

        git_command(worktree, "init", "--initial-branch=main")
        git_command(worktree, "remote", "add", "origin", remote)

        for relative in ("README.md", "app.py", "Dockerfile", "tests"):
            copy_seed(worktree, relative)
        git_command(worktree, "add", "README.md", "app.py", "Dockerfile", "tests")
        git_command(
            worktree,
            "commit",
            "-m",
            "feat: add ORCHID status service",
            env=commit_environment(
                "Alice Mercer", "alice@range.test", "2026-01-12T09:00:00Z"
            ),
        )

        copy_seed(worktree, "config")
        git_command(worktree, "add", "config")
        git_command(
            worktree,
            "commit",
            "-m",
            "chore: add service classification",
            env=commit_environment(
                "Bob Chen", "bob@range.test", "2026-01-13T14:30:00Z"
            ),
        )

        copy_seed(worktree, ".gitea")
        git_command(worktree, "add", ".gitea")
        git_command(
            worktree,
            "commit",
            "-m",
            "ci: publish the ORCHID service image",
            env=commit_environment(
                "Range Administrator",
                "rangeadmin@range.test",
                "2026-01-14T11:15:00Z",
            ),
        )
        git_command(
            worktree,
            "tag",
            "-a",
            SEED_TAG,
            "-m",
            "ORCHID seed release",
            env=commit_environment(
                "Range Administrator",
                "rangeadmin@range.test",
                "2026-01-14T11:16:00Z",
            ),
        )
        git_command(worktree, "push", "origin", "main", "--tags")
        tagged_commit = git_command(
            worktree, "rev-parse", f"{SEED_TAG}^{{}}"
        ).stdout.strip()

        git_command(worktree, "switch", "-c", "feature/telemetry-export")
        copy_seed(worktree, "telemetry.py")
        git_command(worktree, "add", "telemetry.py")
        git_command(
            worktree,
            "commit",
            "-m",
            "feat: add telemetry export identifier",
            env=commit_environment(
                "Alice Mercer", "alice@range.test", "2026-01-15T16:45:00Z"
            ),
        )
        git_command(worktree, "push", "origin", "feature/telemetry-export")
        print("CHANGED created versioned Gitea repository history")
        return tagged_commit


def ensure_collaborator(username: str) -> None:
    """Grant one LDAP-backed user write access to the seeded repository."""
    api(
        f"/repos/{REPOSITORY}/collaborators/{username}",
        method="PUT",
        body={"permission": "write"},
        allowed=(204,),
    )


def ensure_labels() -> dict[str, int]:
    """Create the stable issue-label set and return its IDs."""
    wanted = {
        "bug": "d73a4a",
        "enhancement": "2da44e",
        "restricted": "8b5cf6",
    }
    labels = {item["name"]: item for item in api(f"/repos/{REPOSITORY}/labels")}
    for name, color in wanted.items():
        if name not in labels:
            labels[name] = api(
                f"/repos/{REPOSITORY}/labels",
                method="POST",
                body={
                    "name": name,
                    "color": color,
                    "description": "Synthetic seed label",
                },
                allowed=(201,),
            )
            print(f"CHANGED created Gitea label {name}")
    return {name: int(labels[name]["id"]) for name in wanted}


def ensure_milestone() -> int:
    """Create the stable ORCHID milestone and return its ID."""
    milestones = api(f"/repos/{REPOSITORY}/milestones?state=all")
    for milestone in milestones:
        if milestone["title"] == "ORCHID v1.0":
            return int(milestone["id"])
    milestone = api(
        f"/repos/{REPOSITORY}/milestones",
        method="POST",
        body={
            "title": "ORCHID v1.0",
            "description": "Synthetic partner review milestone",
        },
        allowed=(201,),
    )
    print("CHANGED created Gitea milestone ORCHID v1.0")
    return int(milestone["id"])


def ensure_issue(
    title: str,
    body: str,
    labels: list[int],
    milestone: int,
    auth: tuple[str, str],
) -> dict[str, Any]:
    """Create one issue if an issue with its stable title does not exist."""
    issues = api(f"/repos/{REPOSITORY}/issues?state=all&limit=50", auth=auth)
    for issue in issues:
        if issue["title"] == title and not issue.get("pull_request"):
            return issue
    issue = api(
        f"/repos/{REPOSITORY}/issues",
        method="POST",
        body={"title": title, "body": body, "labels": labels, "milestone": milestone},
        auth=auth,
        allowed=(201,),
    )
    print(f"CHANGED created Gitea issue {title}")
    return issue


def ensure_pull_request(auth: tuple[str, str]) -> dict[str, Any]:
    """Create the stable open telemetry pull request."""
    pulls = api(f"/repos/{REPOSITORY}/pulls?state=all&limit=50", auth=auth)
    for pull in pulls:
        if pull["title"] == "Add telemetry export":
            return pull
    pull = api(
        f"/repos/{REPOSITORY}/pulls",
        method="POST",
        body={
            "title": "Add telemetry export",
            "body": "Synthetic review request for the telemetry export path.",
            "head": "feature/telemetry-export",
            "base": "main",
        },
        auth=auth,
        allowed=(201,),
    )
    print("CHANGED created Gitea pull request")
    return pull


def ensure_comment(issue: dict[str, Any], auth: tuple[str, str]) -> None:
    """Add Bob's stable collaboration comment once."""
    comments = api(f"/repos/{REPOSITORY}/issues/{issue['number']}/comments")
    marker = "scope-seed-v1-review"
    if any(marker in item.get("body", "") for item in comments):
        return
    api(
        f"/repos/{REPOSITORY}/issues/{issue['number']}/comments",
        method="POST",
        body={"body": f"Confirmed against the synthetic telemetry sample. [{marker}]"},
        auth=auth,
        allowed=(201,),
    )
    print("CHANGED created Gitea review comment")


def ensure_actions_configuration() -> None:
    """Create stable repository-level Actions variables and secrets."""
    variables = {
        "REGISTRY_URL": "localhost:5000",
        "SEED_VERSION": SEED_VERSION,
    }
    for name, value in variables.items():
        current = api(
            f"/repos/{REPOSITORY}/actions/variables/{name}", allowed=(200, 404)
        )
        api(
            f"/repos/{REPOSITORY}/actions/variables/{name}",
            method="PUT" if current is not None else "POST",
            body={"value": value},
            allowed=(201, 204),
        )
    for name, value in {
        "REGISTRY_PASSWORD": "ScopeRegistry2026!",
        "GARAGE_SECRET_KEY": "ScopeGarageSecretKey2026ScopeGarageSecretKey2026",
    }.items():
        api(
            f"/repos/{REPOSITORY}/actions/secrets/{name}",
            method="PUT",
            body={"data": value},
            allowed=(201, 204),
        )


def wait_for_published_image() -> None:
    """Wait until the seeded workflow publishes both expected image tags.

    Gitea 1.22 does not expose workflow runs through its REST API. Requiring the
    workflow's final registry artifacts provides a version-compatible success
    signal while still proving that the test, build, and publish job completed.
    """
    deadline = time.monotonic() + 900
    while time.monotonic() < deadline:
        try:
            with urllib.request.urlopen(
                "http://127.0.0.1:5000/v2/orchid-api/tags/list", timeout=30
            ) as response:
                payload = json.load(response)
        except urllib.error.HTTPError as exc:
            if exc.code != 404:
                raise RuntimeError(
                    f"OCI registry tag lookup failed: HTTP {exc.code}"
                ) from exc
        else:
            tags = set(payload.get("tags") or [])
            if {"0.1.0", "latest"}.issubset(tags):
                return
        time.sleep(10)
    raise RuntimeError("timed out waiting for the seeded Gitea Action image")


def upload_release_asset(release: dict[str, Any], content: bytes) -> None:
    """Attach the deterministic build manifest to the Gitea release once."""
    name = "build-manifest.json"
    if any(asset.get("name") == name for asset in release.get("assets", [])):
        return
    boundary = f"scope-{uuid.uuid4().hex}"
    payload = (
        (
            f"--{boundary}\r\n"
            f'Content-Disposition: form-data; name="attachment"; filename="{name}"\r\n'
            "Content-Type: application/json\r\n\r\n"
        ).encode()
        + content
        + f"\r\n--{boundary}--\r\n".encode()
    )
    request = urllib.request.Request(
        f"{GITEA_URL}/api/v1/repos/{REPOSITORY}/releases/{release['id']}/assets?name={name}",
        data=payload,
        method="POST",
        headers={
            "Authorization": authorization(ADMIN_AUTH),
            "Content-Type": f"multipart/form-data; boundary={boundary}",
        },
    )
    with urllib.request.urlopen(request, timeout=30) as response:
        if response.status != 201:
            raise RuntimeError(f"Gitea release asset upload returned {response.status}")
    print("CHANGED attached the build manifest to the Gitea release")


def ensure_release(tagged_commit: str) -> None:
    """Create the stable release and attach its build manifest."""
    release = api(f"/repos/{REPOSITORY}/releases/tags/{SEED_TAG}", allowed=(200, 404))
    if release is None:
        release = api(
            f"/repos/{REPOSITORY}/releases",
            method="POST",
            body={
                "tag_name": SEED_TAG,
                "target_commitish": "main",
                "name": "ORCHID 0.1.0",
                "body": "Synthetic release generated by scope-seed-v1.",
                "draft": False,
                "prerelease": False,
            },
            allowed=(201,),
        )
        print("CHANGED created the Gitea release")
    manifest = json.dumps(
        {
            "image": "orchid-api:0.1.0",
            "seed_version": SEED_VERSION,
            "source_commit": tagged_commit,
        },
        indent=2,
        sort_keys=True,
    ).encode()
    upload_release_asset(release, manifest)


def main() -> None:
    """Seed every versioned Gitea development fixture."""
    for username, (_, password) in LDAP_AUTH.items():
        ensure_ldap_login(username, password)
        ensure_collaborator(username)

    tagged_commit = ensure_repository_history()
    labels = ensure_labels()
    milestone = ensure_milestone()
    first_issue = ensure_issue(
        "Telemetry export omits calibration metadata",
        "The partner export needs the synthetic calibration identifier.",
        [labels["bug"], labels["restricted"]],
        milestone,
        LDAP_AUTH["alice"],
    )
    ensure_issue(
        "Document registry rollback procedure",
        "Add a recovery note for the local ORCHID image registry.",
        [labels["enhancement"]],
        milestone,
        LDAP_AUTH["bob"],
    )
    ensure_issue(
        "Review partner release checklist",
        "Confirm the fictional Northstar review artifacts are complete.",
        [labels["restricted"]],
        milestone,
        ADMIN_AUTH,
    )
    ensure_comment(first_issue, LDAP_AUTH["bob"])
    ensure_pull_request(LDAP_AUTH["alice"])
    ensure_actions_configuration()
    wait_for_published_image()
    ensure_release(tagged_commit)


if __name__ == "__main__":
    main()
