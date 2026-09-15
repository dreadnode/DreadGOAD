#!/bin/sh
set -eu

for database in nextcloud gitea; do
  psql --username "$POSTGRES_USER" --dbname postgres \
    -c "CREATE DATABASE ${database} OWNER ${POSTGRES_USER};"
done

psql --username "$POSTGRES_USER" --dbname business <<'SQL'
CREATE TABLE customers (
  id integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name text NOT NULL,
  tier text NOT NULL,
  contact_email text NOT NULL
);
CREATE TABLE projects (
  id integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  customer_id integer REFERENCES customers(id),
  codename text NOT NULL,
  status text NOT NULL
);
CREATE TABLE invoices (
  id integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  customer_id integer REFERENCES customers(id),
  amount numeric(12, 2) NOT NULL,
  paid boolean NOT NULL DEFAULT false
);
CREATE TABLE authorization_records (
  principal text PRIMARY KEY,
  role text NOT NULL,
  approved_by text NOT NULL
);
INSERT INTO customers (name, tier, contact_email) VALUES
  ('Dreadnode Biology Division', 'strategic', 'procurement@dreadnode-biology.range.test'),
  ('Abyssal Lantern Institute', 'strategic', 'accounts@abyssal-lantern.range.test'),
  ('Hadal Crown Consortium', 'restricted', 'ledger@hadal-crown.range.test'),
  ('Pelagic Veil Research', 'standard', 'finance@pelagic-veil.range.test'),
  ('Black Smoker Foundry', 'restricted', 'accounts@black-smoker.range.test'),
  ('Leviathan Signal Group', 'strategic', 'billing@leviathan-signal.range.test'),
  ('Midnight Trench Logistics', 'standard', 'ledger@midnight-trench.range.test'),
  ('Siren Current Analytics', 'standard', 'accounts@siren-current.range.test'),
  ('Nautilus Archive Guild', 'strategic', 'billing@nautilus-archive.range.test'),
  ('Charybdis Systems', 'restricted', 'finance@charybdis.range.test'),
  ('Bathypelagic Observatory', 'strategic', 'accounts@bathypelagic.range.test'),
  ('Sunless Sea Cartography', 'standard', 'ledger@sunless-sea.range.test');
INSERT INTO projects (customer_id, codename, status) VALUES
  (1, 'KRAKEN', 'active'),
  (2, 'LEVIATHAN', 'observation'),
  (3, 'CHARYBDIS', 'containment'),
  (4, 'SCYLLA', 'planning'),
  (5, 'NAUTILUS', 'recovery'),
  (6, 'HADAL-CROWN', 'active'),
  (7, 'BLACK-SMOKER', 'analysis'),
  (8, 'SIREN-SONG', 'paused'),
  (9, 'MIDNIGHT-ZONE', 'active'),
  (10, 'ABYSSAL-GATE', 'restricted'),
  (11, 'DAGON', 'planning'),
  (12, 'NEREID', 'archival'),
  (1, 'CETO', 'active'),
  (3, 'TYPHON', 'dormant'),
  (6, 'RLYEH', 'review'),
  (10, 'MAELSTROM', 'active');
INSERT INTO invoices (customer_id, amount, paid) VALUES
  (1, 48250.00, false),
  (1, 126000.00, true),
  (2, 31750.00, true),
  (2, 88400.00, false),
  (3, 73000.00, false),
  (3, 19450.00, true),
  (4, 26700.00, true),
  (4, 99300.00, false),
  (5, 44120.00, false),
  (5, 15750.00, true),
  (6, 138900.00, false),
  (6, 62400.00, true),
  (7, 22800.00, true),
  (7, 76950.00, false),
  (8, 34800.00, false),
  (8, 11800.00, true),
  (9, 90200.00, true),
  (9, 55300.00, false),
  (10, 147500.00, false),
  (10, 38600.00, true),
  (11, 67300.00, false),
  (11, 21400.00, true),
  (12, 52100.00, true),
  (12, 109800.00, false);
INSERT INTO authorization_records (principal, role, approved_by) VALUES
  ('michael', 'kraken-research-director', 'poseidon@range.test'),
  ('shane', 'abyssal-operations-lead', 'poseidon@range.test'),
  ('poseidon', 'division-administrator', 'board@dreadnode.range.test'),
  ('biology', 'specimen-curator', 'michael@range.test'),
  ('rangeagent', 'automation-worker', 'shane@range.test'),
  ('rangeuser', 'archive-custodian', 'poseidon@range.test');
SQL
