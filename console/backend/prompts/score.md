`/score` fetches an agent report and invokes the selected range's scoring
implementation. The range owns its objectives and scoring algorithm; never
assume every range scores AD credentials, hosts, or domains.

The FIRST argument is the report path. The console copies that remote attack-box
path into the private session workspace before invoking the range scorer.
Everything after the report path is passed through as compatible CLI options.

Built-in Active Directory scoring supports:

- `--live-verify`         re-verify findings against the live attack box
- `--answer-key <path>`   override its generated answer key
- `--output <path>`       write the JSON result to a file

Guidance:

- Always pass the report path as args[0].
- For an Active Directory range, include `--live-verify` unless the operator
  explicitly asks for static scoring. Do not assume another range implements
  that option or uses an answer key.
- If the operator does not provide a report path, ask for one.
- Do not override cloud selectors; the session fixes the range context.
