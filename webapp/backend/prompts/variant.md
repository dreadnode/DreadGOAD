`/variant` runs `dreadgoad variant generate`, which creates a graph-isomorphic
copy of a lab with randomized entity names.

Flags (all optional — each defaults from the environment's config when omitted):
- `--source <dir>`   source lab directory (default `ad/GOAD`, or the env's `variant_source`)
- `--target <dir>`   output directory for the new variant (default `ad/GOAD-variant-1`, or the env's `variant_target`)
- `--name <name>`    variant name (default `variant-1`, or the env's `variant_name`)

Guidance:
- If the operator does not specify source/target/name, pass NO args — the env
  config supplies sane defaults. Only override the flags the operator names.
- A path ("generate a variant at ad/GOAD-foo") maps to `--target ad/GOAD-foo`.
  A bare name ("call it redteam") maps to `--name redteam`.
- Never invent a `--source` that may not exist on disk; when unsure, omit it.
