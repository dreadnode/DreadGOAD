`/score` runs `dreadgoad score --report <path>`, scoring an agent's JSONL report
against the answer key.

The FIRST argument you pass is the report path; the console handles fetching it
(a remote path on the attack box is copied locally automatically before scoring).
Everything after the report path is passed through to the CLI as flags.

Useful flags:
- `--live-verify`         re-verify findings live against the attack box
- `--answer-key <path>`   override the answer key (default `scoreboard/answer_key.json`)
- `--output <path>`       write the JSON result to a file instead of stdout

Guidance:
- Always pass the report path as args[0].
- **Always include `--live-verify` unless the operator explicitly says not to.**
  Static scoring alone misses credentials the agent changed during exploitation
  (password resets, shadow credentials). Live verification is the only way to
  confirm those, and it is what the operator expects when they ask to score a run.
  Example: score `/root/report.jsonl` → args = `["/root/report.jsonl", "--live-verify"]`.
- If the operator doesn't give a report path, ask for one — do not guess.
- Do NOT pass `--attack-box`/`--region`/`--ssh-key`; the range's cloud context is
  resolved for you.
