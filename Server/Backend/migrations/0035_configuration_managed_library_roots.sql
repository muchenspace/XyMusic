ALTER TABLE library_roots
  ADD COLUMN configuration_managed boolean NOT NULL DEFAULT false;

-- Databases created before multiple managed roots existed had one implicit
-- configuration-owned root. Preserve that root while treating later roots as
-- administrator-owned, so a worker reload cannot overwrite them.
UPDATE library_roots
SET configuration_managed = true
WHERE id = (
  SELECT id
  FROM library_roots
  ORDER BY created_at ASC, id ASC
  LIMIT 1
);