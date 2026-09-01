`/restart <host>` reboots ONE machine through the cloud provider's power API,
leaving every other host in the range running. It maps to `lab restart-vm`.

## Usage

Pass the hostname as the first argument: `/restart dc02`. It is resolved by
`FindInstanceByHostname`, so use the short name the range knows (`dc02`,
`srv03`), not the provider's full VM name.

Exactly one host per invocation. There is no "all" form — use `/stop` then
`/start` if the whole range genuinely needs cycling.

## When this is the right tool

A hard power cycle is the fix when a host is too wedged for anything softer:

- Out of memory. The signature is a host that pings and shows `running` in
  `/instances`, but where `/exec` returns errors like "the paging file is too
  small" or a failure to load `System.Management.Automation` — the OS can no
  longer start a process, so nothing in-guest can repair it.
- WinRM refusing connections while the VM is up, after `/exec` confirms the
  service cannot be started.
- A host stuck mid-boot, or one whose guest agent has stopped responding.

Reach for it only after inspection has told you *why*. "It's failing, reboot it"
is a guess; "it's out of memory and cannot start a process, so nothing in-guest
can fix it" is a reason.

## What it costs

The host is hard-reset — anything unsaved in memory is lost, and Active
Directory on that machine goes offline for the reboot. On a domain controller
that means clients briefly fail to authenticate against it and replication
pauses. Say so before running it, and confirm with the operator.

It does NOT touch disks, configuration, or AD data. A reboot is recoverable;
this is far safer than `/reset` or `/provision`.

## Afterwards

1. Give it a couple of minutes — the API returns as soon as the reset is issued,
   not when the OS is back.
2. `/instances` to confirm it is running again.
3. `/health` to confirm AD is actually serving, which is the real test.
4. If the same fault returns shortly after, the reboot only cleared a symptom.
   Say so rather than rebooting a second time — a host that exhausts memory
   again needs a resize or a look at what is consuming it.
