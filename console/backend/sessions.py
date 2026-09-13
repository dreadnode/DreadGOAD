"""Session lifecycle service (design §4.2, §4.3, §7).

A session = a ``(config_path, env)`` anchor + a derived snapshot + a working
dir. Create/list/get/delete over the SQLite layer; topology is seeded at
create time (config hosts if the lab exists, infra nodes otherwise).
"""

from __future__ import annotations

import asyncio
import contextlib
import ipaddress
import os
import re
import shutil
import stat
import typing as t
import uuid
from datetime import datetime, timezone
from pathlib import Path

from . import configstore, labconfig, labs, lifecycle, paths, projectroot, scaffold
from .db import Database
from .schemas import RangeDocument, SessionDocument, SessionSnapshot


def _slug(s: str) -> str:
    return re.sub(r"[^a-z0-9]+", "-", (s or "").lower()).strip("-")[:40] or "session"


def _now() -> str:
    return datetime.now(timezone.utc).isoformat()


_ENV_NAME_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]*$")
_RFC1918_NETWORKS: tuple[ipaddress.IPv4Network, ...] = tuple(
    ipaddress.IPv4Network(cidr)
    for cidr in ("10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16")
)


def _validate_path_component(label: str, value: str) -> str:
    value = value.strip()
    if not value or not _ENV_NAME_RE.fullmatch(value):
        raise ValueError(
            f"{label} {value!r} is invalid; use letters, digits, dots, "
            "hyphens, and underscores"
        )
    return value


def _deterministic_cidr(env: str) -> str:
    """Mirror Config.VpcCIDR for ASCII environment names."""
    value = 0
    for char in env:
        value = (value * 31 + ord(char)) & 0xFF
    return f"10.{value % 240 + 10}.0.0/16"


def _validate_cidr(value: str) -> str:
    """Validate the /16 IPv4 network assumed by both cloud scaffolders."""
    try:
        network = ipaddress.ip_network(value, strict=True)
    except ValueError as exc:
        raise ValueError(
            f"VPC/VNet CIDR {value!r} is invalid; use a network such as 10.50.0.0/16"
        ) from exc
    if network.version != 4 or network.prefixlen != 16:
        raise ValueError(
            f"VPC/VNet CIDR {value!r} is unsupported; range scaffolding "
            "currently requires an IPv4 /16"
        )
    if not any(network.subnet_of(private) for private in _RFC1918_NETWORKS):
        raise ValueError(
            f"VPC/VNet CIDR {value!r} is not private; use RFC 1918 address space"
        )
    return str(network)


def default_label(config_path: str, env: str, snapshot: SessionSnapshot) -> str:
    """The tab name for a session, as ``<config>/<env>``.

    A tab only has to answer *which session is this*; what it is — provider,
    region, resource group, account — is already on screen in the range header
    (RangeView's fields). The previous format spent its width the other way
    round, on ``<env> · <provider>/<variant name>``: it repeated the provider
    the header was showing, omitted the config entirely, and ended in the
    variant name, which defaults to the environment name and so restated it.

    The config is the axis that actually distinguishes sessions now that more
    than one can exist, and it was the only thing not shown at all.

    The variant is appended only when it differs from the environment name,
    which is the sole case where that third segment carries information.

    There is no rename endpoint, so this is the name a session keeps for life.
    """
    stem = Path(config_path).stem or "config"
    name = f"{stem}/{env}"
    variant_name = snapshot.get("variant_name")
    if variant_name and variant_name != env:
        name = f"{name} · {variant_name}"
    return name


class SessionService:
    """Create, read, and delete sessions over the SQLite layer.

    Owns the session's derived snapshot, its working directory, and the initial
    range topology seeded from the lab config at creation time.
    """

    def __init__(
        self, db: Database, repo_root: str | Path, sessions_root: str | Path
    ) -> None:
        self.db = db
        self.repo_root = str(repo_root)
        self.sessions_root = paths.ensure_private_dir(sessions_root)
        for child in self.sessions_root.iterdir():
            # Repair only real directories. Never follow an unexpected symlink
            # while tightening modes on state left by an older console.
            if stat.S_ISDIR(child.lstat().st_mode):
                paths.ensure_private_dir(child)

    async def create_session(
        self,
        config_path: str,
        env: str,
        model: str | None = None,
        label: str | None = None,
    ) -> SessionDocument:
        """Attach a session to an existing ``(config_path, env)``."""
        snap = labconfig.derive_snapshot(config_path, env)

        sid = "s-" + uuid.uuid4().hex[:8]
        lbl = label or default_label(config_path, env, snap)
        dirname = f"{_slug(label or env)}-{sid[2:]}"
        sdir = paths.ensure_private_dir(self.sessions_root / dirname)

        session: SessionDocument = {
            "id": sid,
            "label": lbl,
            "model": model,
            "status": "new",
            "anchor": {"config_path": str(config_path), "env": env},
            "snapshot": snap,
            "session_dir": str(sdir),
            "created_at": _now(),
            "updated_at": _now(),
        }
        await self.db.upsert_session(session)

        topo = self._seed_topology(session)
        topo["session_id"] = sid
        await self.db.upsert_range(sid, topo)

        await self.db.append_event(sid, "session_created", {"label": lbl})
        await self._initialize(session)
        return session

    async def create_new_env_session(
        self,
        config_path: str,
        env_name: str,
        env_fields: dict[str, t.Any],
        top_level: dict[str, t.Any] | None = None,
        model: str | None = None,
        label: str | None = None,
    ) -> SessionDocument:
        """Create-new-env flow: write the env into the yaml, then attach."""
        if bool(env_fields.get("variant")):
            root, _ = projectroot.resolve_root(config_path)
            labs.require_variant_source_supported(
                root, str(env_fields.get("variant_source") or "ad/GOAD")
            )
        labconfig.write_new_env(config_path, env_name, env_fields, top_level)
        session = await self.create_session(
            config_path, env_name, model=model, label=label
        )
        await self._scaffold_for(session, env_fields)
        return session

    async def _scaffold_for(
        self, session: SessionDocument, env_fields: dict[str, t.Any]
    ) -> tuple[bool, str]:
        """Build the environment's infrastructure and record what happened.

        Failure is deliberately NOT fatal to session creation. The session and
        its config entry are already valid and useful — the operator can inspect
        them, fix whatever the scaffold complained about, and retry — whereas
        unwinding a successful create because a later step failed would throw
        away the part that worked. The outcome is recorded as an event so it is
        in the chat history rather than only in a response body nobody reads.
        """
        snapshot = session.get("snapshot") or {}
        region = snapshot.get("region")
        anchor = session["anchor"]
        if not region:
            await self.db.append_event(
                session["id"],
                "status",
                {
                    "content": (
                        "Skipped infrastructure scaffolding: no region is set for "
                        "this environment, and env create requires one."
                    )
                },
            )
            return False, "no region is set for this environment"

        ok, output = await scaffold.scaffold_env(
            str(anchor["config_path"]),
            str(anchor["env"]),
            str(region),
            variant=bool(env_fields.get("variant")),
            variant_source=env_fields.get("variant_source"),
            variant_target=env_fields.get("variant_target"),
            vpc_cidr=env_fields.get("vpc_cidr"),
            provider=str(snapshot.get("provider") or ""),
            deployment=str(snapshot.get("deployment") or "goad-deployment"),
        )
        await self.db.append_event(
            session["id"],
            "status" if ok else "error",
            {
                "content": (
                    f"Infrastructure scaffolded for {anchor['env']}.\n{output}"
                    if ok
                    else (
                        f"Infrastructure was NOT scaffolded for {anchor['env']}, so "
                        f"/up will fail until it is.\n{output}"
                    )
                )
            },
        )
        if ok:
            # A variant's generated config does not exist when create_session
            # first runs, so its declared actions report pending and need this
            # post-scaffold retry. Ordinary ranges were already initialized at
            # session creation; rerunning them here would duplicate events and
            # unnecessarily rewrite generated artifacts.
            if bool(env_fields.get("variant")):
                await self._initialize(session)
            # The variant only exists now, so the topology seeded during
            # create_session above saw no lab config and holds infra nodes only.
            await self._reseed_topology(session)
        return ok, output

    async def create_range_session(
        self,
        range_name: str,
        provider: str,
        env_name: str,
        *,
        region: str | None = None,
        customization: str = "standard",
        vpc_cidr: str | None = None,
        model: str | None = None,
        label: str | None = None,
    ) -> SessionDocument:
        """Create a range-first managed environment and attach a session."""
        env_name = _validate_path_component("environment name", env_name)
        range_name = _validate_path_component("range name", range_name)
        if provider not in configstore.PROVIDERS:
            raise ValueError(
                f"provider must be one of {', '.join(configstore.PROVIDERS)}"
            )

        catalog = await labs.discover_labs()
        selected = next(
            (entry for entry in catalog if entry.get("name") == range_name), None
        )
        if selected is None or selected.get("generated"):
            raise ValueError(f"unknown base range {range_name!r}")
        settings = (selected.get("provider_settings") or {}).get(provider)
        if not isinstance(settings, dict):
            raise ValueError(
                f"range {range_name} cannot be created with provider {provider}"
            )

        deployment = _validate_path_component(
            "deployment", str(settings.get("deployment") or "")
        )
        effective_region = _validate_path_component(
            "region", str(region or settings.get("default_region") or "")
        )
        network = settings.get("network") or {}
        fixed_cidr = str(network.get("cidr") or "")
        editable = network.get("editable") is not False
        requested_cidr = (vpc_cidr or "").strip()
        if not editable and requested_cidr and requested_cidr != fixed_cidr:
            raise ValueError(
                f"range {range_name} requires VPC/VNet CIDR {fixed_cidr}; "
                f"got {requested_cidr}"
            )
        effective_cidr = fixed_cidr if not editable else requested_cidr
        effective_cidr = effective_cidr or fixed_cidr or _deterministic_cidr(env_name)
        effective_cidr = _validate_cidr(effective_cidr)

        if customization not in ("standard", "randomized"):
            raise ValueError("customization must be 'standard' or 'randomized'")
        use_variant = customization == "randomized"
        if use_variant and selected.get("variant_supported") is not True:
            raise ValueError(f"range {range_name} does not support randomized variants")

        env_fields: dict[str, t.Any] = {
            "lab": range_name,
            "provider": provider,
            "deployment": deployment,
            "region": effective_region,
            "vpc_cidr": effective_cidr,
            "variant": use_variant,
        }
        variant_target: str | None = None
        if use_variant:
            variant_target = f"ad/{range_name}-{env_name}"
            env_fields.update(
                {
                    "variant_source": f"ad/{range_name}",
                    "variant_target": variant_target,
                    "variant_name": env_name,
                }
            )

        problems = scaffold.preflight(
            self.repo_root,
            provider,
            env_name,
            variant_target,
            deployment=deployment,
        )
        if problems:
            raise FileExistsError("\n".join(problems))

        path = str(configstore.managed_path_for(range_name, provider, env_name))
        labconfig.create_managed_config(path, env_name, env_fields)
        try:
            session = await self.create_session(
                path,
                env_name,
                model=model,
                label=label
                or f"{env_name} · {selected.get('display_name') or range_name}",
            )
        except Exception:
            with contextlib.suppress(OSError):
                os.unlink(path)
            raise

        try:
            ok, output = await self._scaffold_for(session, env_fields)
            if not ok:
                raise ValueError(
                    f"could not prepare {range_name} environment {env_name}: {output}"
                )
        except Exception:
            # This flow creates the config and session solely as preparation
            # for the requested range. A failed or interrupted scaffold must
            # unwind both so the same range name can be retried cleanly.
            with contextlib.suppress(Exception):
                await self.delete_session(session["id"])
            with contextlib.suppress(OSError):
                os.unlink(path)
            raise
        return session

    async def _initialize(self, session: SessionDocument) -> None:
        """Run and retain this range's declarative session initialization."""
        try:
            results = await lifecycle.initialize_session(session, self.repo_root)
        except Exception as exc:  # noqa: BLE001 - initialization is non-fatal
            results = [
                {
                    "action": "range_init",
                    "status": "failed",
                    "message": str(exc),
                }
            ]
        for result in results:
            action = result["action"].replace("_", " ")
            detail = result.get("message", "")
            content = f"Session initialization: {action} {result['status']}."
            if detail:
                content += f" {detail}"
            await self.db.append_event(
                session["id"],
                "status",
                {"content": content, "initialization": result},
            )

    async def create_config_session(
        self,
        config_name: str,
        provider: str,
        env_name: str,
        env_fields: dict[str, t.Any],
        region: str | None = None,
        model: str | None = None,
        label: str | None = None,
    ) -> SessionDocument:
        """Create-new-config flow: write a config, put one env in it, attach.

        The whole point of doing this in one call rather than exposing a
        separate config endpoint is that a config with no environment in it is
        not a thing anyone wants — it cannot be attached to, so it would only
        ever be a half-finished state for the UI to explain.

        Unlike :meth:`create_new_env_session`, a failure after the write is
        rolled back. The file is one this call exclusively created (create_config
        opens O_EXCL), so deleting it destroys nothing that existed before — and
        leaving it would be worse than untidy: create_config refuses to overwrite,
        so retrying with the same name would then fail with "already exists" for
        a config the operator never successfully made.
        """
        path = str(configstore.path_for(config_name))
        if bool(env_fields.get("variant")):
            labs.require_variant_source_supported(
                self.repo_root,
                str(env_fields.get("variant_source") or "ad/GOAD"),
            )
        labconfig.create_config(path, provider, env_name, env_fields, region=region)
        try:
            session = await self.create_session(
                path, env_name, model=model, label=label
            )
        except Exception:
            with contextlib.suppress(OSError):
                os.unlink(path)
            raise
        await self._scaffold_for(session, env_fields)
        return session

    def _seed_topology(self, session: SessionDocument) -> RangeDocument:
        cfg = labconfig.session_lab_config_path(session, self.repo_root)
        return labconfig.seed_topology(
            cfg, (session.get("snapshot") or {}).get("provider")
        )

    async def _reseed_topology(self, session: SessionDocument) -> None:
        """Re-read the lab config into the topology, keeping live state.

        Seeding happens inside create_session, which runs BEFORE the scaffold
        that generates the variant — so a newly created environment is seeded
        from a lab config that does not exist yet and comes up with infra nodes
        only. Nothing else re-reads it: inventory_sync overlays cloud state onto
        hosts already in the topology and never adds one ("Overlay cloud state
        without adding instances absent from the topology"), so a host missing
        from the seed stays invisible no matter how many times the hook runs.

        merge_reseed keeps status/ip/layout for surviving nodes, so this is safe
        to run over a topology the operator has already arranged.
        """
        rng = await self.db.get_range(session["id"])
        if rng is None:
            return
        seeded = self._seed_topology(session)
        await self.db.upsert_range(session["id"], labconfig.merge_reseed(rng, seeded))

    async def list_sessions(self) -> list[SessionDocument]:
        """Return every known session."""
        return await self.db.list_sessions()

    async def get_session(self, session_id: str) -> SessionDocument | None:
        """Return one session, or None if it doesn't exist."""
        return await self.db.get_session(session_id)

    async def delete_session(self, session_id: str) -> bool:
        """Delete a session, its range/events, and its working dir."""
        session = await self.db.get_session(session_id)
        if session is None:
            return False
        sdir = session.get("session_dir")
        if sdir:
            root = self.sessions_root.resolve()
            target = Path(sdir).resolve()
            if target == root or root not in target.parents:
                raise ValueError(
                    "refusing to delete a session dir outside sessions root"
                )
            if target.exists():
                await asyncio.to_thread(shutil.rmtree, target)
        await self.db.delete_session(session_id)
        return True

    async def set_status(self, session_id: str, status: str) -> None:
        """Flush a status-critical write immediately (§6.1 durability)."""
        session = await self.db.get_session(session_id)
        if session is None:
            return
        session["status"] = status
        session["updated_at"] = _now()
        await self.db.upsert_session(session)
