CREATE TABLE wordpress.range_notes (
  id INT AUTO_INCREMENT PRIMARY KEY,
  author VARCHAR(64) NOT NULL,
  note TEXT NOT NULL
);
INSERT INTO wordpress.range_notes (author, note) VALUES
  ('michael', 'Publish the KRAKEN breeding update after the pressure-vault review.'),
  ('shane', 'Archive the old hadal camera feed after migration.'),
  ('poseidon', 'Keep enclosure coordinates out of the public release.'),
  ('michael', 'The KRA-003 quarantine report needs a second signature.'),
  ('shane', 'Confirm the Dreadnought Bathyscaphe manifest before the next dive.'),
  ('poseidon', 'Move the RLYEH acoustic samples into restricted storage.');
