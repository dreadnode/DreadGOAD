`/extensions` maps to the `dreadgoad extension` command. The console shapes it
from your args:
- args = `[]`            → `extension list` (show available extensions)
- args = `[<name>, …]`   → `extension provision <name>` (provision that extension)

So the FIRST argument, if present, is ALWAYS treated as the extension NAME to
provision (e.g. `elk`, `exchange`, `guacamole`). Flags after it pass through:
- `--limit <hosts>`      limit provisioning to specific hosts
- `--max-retries <n>`    retry attempts
- `--retry-delay <sec>`  delay between retries (seconds)

Guidance:
- To LIST extensions, pass NO args.
- To PROVISION, args[0] must be the extension name (never a flag). Example:
  provision ELK limited to dc01 → args = `["elk", "--limit", "dc01"]`.
- This path provisions ONE named extension at a time; there is no provision-all.
