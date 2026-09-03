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
  ('Northstar Research', 'strategic', 'procurement@northstar.range.test'),
  ('Blue Mesa Analytics', 'standard', 'accounts@bluemesa.range.test');
INSERT INTO projects (customer_id, codename, status) VALUES
  (1, 'ORCHID', 'active'),
  (2, 'LANTERN', 'planning');
INSERT INTO invoices (customer_id, amount, paid) VALUES
  (1, 48250.00, false),
  (2, 9600.00, true);
INSERT INTO authorization_records (principal, role, approved_by) VALUES
  ('alice', 'research-admin', 'director@northstar.range.test'),
  ('bob', 'billing-analyst', 'cfo@bluemesa.range.test');
SQL
