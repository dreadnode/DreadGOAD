CREATE TABLE wordpress.range_notes (
  id INT AUTO_INCREMENT PRIMARY KEY,
  author VARCHAR(64) NOT NULL,
  note TEXT NOT NULL
);
INSERT INTO wordpress.range_notes (author, note) VALUES
  ('alice', 'Publish the ORCHID project update after the review.'),
  ('bob', 'Archive the old partner media after migration.');
