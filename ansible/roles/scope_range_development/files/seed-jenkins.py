#!/usr/bin/env python3
"""Create Jenkins credentials and a successful versioned synthetic build."""

from __future__ import annotations

import base64
import http.cookiejar
import json
import pathlib
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET
from typing import Any


JENKINS_URL = "http://127.0.0.1:8080"
AUTH = ("rangeadmin", "ScopeJenkins2026!")
JOB_NAME = "orchid-nightly-export"
JOB_CONFIG = pathlib.Path("/opt/scope-range/development/orchid-nightly-export.xml")
GARAGE_ARTIFACT_ROOT = "http://s3.range.test:3900/build-artifacts/orchid/0.1.0"
GARAGE_AUTH = (
    "GKSCOPERANGE2026ACCESS",
    "ScopeGarageSecretKey2026ScopeGarageSecretKey2026",
)
OPENER = urllib.request.build_opener(
    urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar())
)


def authorization() -> str:
    """Return the Jenkins HTTP Basic authorization value."""
    encoded = base64.b64encode(f"{AUTH[0]}:{AUTH[1]}".encode()).decode()
    return f"Basic {encoded}"


def request(
    path: str,
    *,
    method: str = "GET",
    data: bytes | None = None,
    content_type: str | None = None,
    allowed: tuple[int, ...] = (200,),
) -> bytes:
    """Call Jenkins with a crumb for mutating requests."""
    headers = {"Authorization": authorization()}
    if content_type:
        headers["Content-Type"] = content_type
    if method != "GET":
        crumb_request = urllib.request.Request(
            f"{JENKINS_URL}/crumbIssuer/api/json",
            headers={"Authorization": authorization()},
        )
        with OPENER.open(crumb_request, timeout=30) as response:
            crumb = json.loads(response.read())
        headers[crumb["crumbRequestField"]] = crumb["crumb"]
    http_request = urllib.request.Request(
        f"{JENKINS_URL}{path}", data=data, method=method, headers=headers
    )
    try:
        with OPENER.open(http_request, timeout=60) as response:
            status = response.status
            payload = response.read()
    except urllib.error.HTTPError as exc:
        if exc.code not in allowed:
            detail = exc.read().decode(errors="replace")
            raise RuntimeError(
                f"Jenkins {method} {path} failed: {exc.code} {detail}"
            ) from exc
        status = exc.code
        payload = exc.read()
    if status not in allowed:
        raise RuntimeError(f"Jenkins {method} {path} returned {status}")
    return payload


def ensure_credentials() -> None:
    """Create the two stable synthetic username/password credentials."""
    groovy = r"""
import com.cloudbees.plugins.credentials.CredentialsScope
import com.cloudbees.plugins.credentials.SystemCredentialsProvider
import com.cloudbees.plugins.credentials.domains.Domain
import com.cloudbees.plugins.credentials.impl.UsernamePasswordCredentialsImpl

def store = SystemCredentialsProvider.getInstance().getStore()
def existing = SystemCredentialsProvider.getInstance().getCredentials().collect { it.id } as Set
def wanted = [
  ["scope-gitea", "rangeadmin", "ScopeGitea2026!", "Synthetic Gitea account"],
  ["scope-garage", "GKSCOPERANGE2026ACCESS", "ScopeGarageSecretKey2026ScopeGarageSecretKey2026", "Synthetic Garage access key"]
]
wanted.each { item ->
  if (!existing.contains(item[0])) {
    store.addCredentials(
      Domain.global(),
      new UsernamePasswordCredentialsImpl(
        CredentialsScope.GLOBAL, item[0], item[3], item[1], item[2]
      )
    )
    println("CHANGED " + item[0])
  }
}
"""
    response = request(
        "/scriptText",
        method="POST",
        data=urllib.parse.urlencode({"script": groovy}).encode(),
        content_type="application/x-www-form-urlencoded",
    ).decode()
    if "CHANGED" in response:
        print("CHANGED created Jenkins synthetic credentials")


def ensure_job() -> None:
    """Create or reconcile the stable Jenkins freestyle job."""
    config = JOB_CONFIG.read_bytes()
    try:
        current = request(f"/job/{JOB_NAME}/config.xml")
    except RuntimeError as exc:
        if "404" not in str(exc):
            raise
        request(
            f"/createItem?name={urllib.parse.quote(JOB_NAME)}",
            method="POST",
            data=config,
            content_type="application/xml",
            allowed=(200,),
        )
        print(f"CHANGED created Jenkins job {JOB_NAME}")
        return
    desired_xml = ET.canonicalize(config.decode(), strip_text=True)
    current_xml = ET.canonicalize(current.decode(), strip_text=True)
    if current_xml != desired_xml:
        request(
            f"/job/{JOB_NAME}/config.xml",
            method="POST",
            data=config,
            content_type="application/xml",
            allowed=(200,),
        )
        print(f"CHANGED updated Jenkins job {JOB_NAME}")


def successful_seed_build_exists() -> bool:
    """Return whether Jenkins and Garage retain the expected build artifacts."""
    try:
        payload = request(
            f"/job/{JOB_NAME}/lastSuccessfulBuild/artifact/seed-output/build-manifest.json"
        )
    except RuntimeError as exc:
        if "404" in str(exc):
            return False
        raise
    try:
        manifest: Any = json.loads(payload)
    except json.JSONDecodeError:
        return False
    if manifest.get("seed_version") != "scope-seed-v1":
        return False
    for name in ("build-manifest.json", "orchid-source.tar.gz"):
        try:
            completed = subprocess.run(
                [
                    "curl",
                    "--fail",
                    "--silent",
                    "--show-error",
                    "--header",
                    "x-amz-content-sha256: UNSIGNED-PAYLOAD",
                    "--aws-sigv4",
                    "aws:amz:range:s3",
                    "--user",
                    f"{GARAGE_AUTH[0]}:{GARAGE_AUTH[1]}",
                    "--output",
                    "/dev/null",
                    f"{GARAGE_ARTIFACT_ROOT}/{name}",
                ],
                check=False,
                capture_output=True,
                timeout=30,
            )
        except subprocess.TimeoutExpired:
            return False
        if completed.returncode != 0:
            return False
    return True


def run_seed_build() -> None:
    """Trigger the job once and wait for a successful result."""
    if successful_seed_build_exists():
        return
    try:
        previous = json.loads(request(f"/job/{JOB_NAME}/lastBuild/api/json"))
        previous_number = int(previous.get("number", 0))
    except RuntimeError as exc:
        if "404" not in str(exc):
            raise
        previous_number = 0
    request(f"/job/{JOB_NAME}/build", method="POST", data=b"", allowed=(201, 302))
    print(f"CHANGED triggered Jenkins job {JOB_NAME}")
    deadline = time.monotonic() + 900
    observed_build = False
    while time.monotonic() < deadline:
        try:
            payload = request(f"/job/{JOB_NAME}/lastBuild/api/json")
        except RuntimeError as exc:
            if "404" in str(exc):
                time.sleep(5)
                continue
            raise
        build = json.loads(payload)
        if int(build.get("number", 0)) <= previous_number:
            time.sleep(5)
            continue
        observed_build = True
        if build.get("building"):
            time.sleep(5)
            continue
        if build.get("result") != "SUCCESS":
            raise RuntimeError(
                f"Jenkins seed build {build.get('number')} concluded {build.get('result')}"
            )
        if not successful_seed_build_exists():
            raise RuntimeError(
                "Jenkins seed build omitted its expected manifest artifact"
            )
        return
    state = "running" if observed_build else "not scheduled"
    raise RuntimeError(f"timed out waiting for Jenkins seed build ({state})")


def main() -> None:
    """Seed credentials, job configuration, and one successful build."""
    ensure_credentials()
    ensure_job()
    run_seed_build()


if __name__ == "__main__":
    main()
