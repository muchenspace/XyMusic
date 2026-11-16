CREATE TABLE dismissed_default_library_roots (
  normalized_path varchar(4000) PRIMARY KEY,
  dismissed_at timestamptz NOT NULL DEFAULT now()
);
