CREATE TYPE lyrics_timing AS ENUM ('LINE', 'WORD');
--> statement-breakpoint
ALTER TABLE lyrics ADD COLUMN timing lyrics_timing NOT NULL DEFAULT 'LINE';
--> statement-breakpoint
CREATE FUNCTION xymusic_detect_lyrics_timing(input_format text, input_content text)
RETURNS lyrics_timing
LANGUAGE sql
IMMUTABLE
AS $$
SELECT CASE
  WHEN input_format = 'LRC'
  AND EXISTS (
    SELECT 1
    FROM regexp_split_to_table(replace(input_content, E'\r', ''), E'\\n') AS raw(line)
    CROSS JOIN LATERAL (
      SELECT regexp_replace(
        raw.line,
        E'^\\s*(\\[[0-9]{1,3}:[0-5][0-9]([.:][0-9]{1,3})?\\])+\\s*',
        ''
      ) AS body
    ) AS parsed
    WHERE raw.line ~ E'^\\s*(\\[[0-9]{1,3}:[0-5][0-9]([.:][0-9]{1,3})?\\])+'
      AND btrim(parsed.body, E' \t\n\r\f\v') <> ''
  )
  AND NOT EXISTS (
    SELECT 1
    FROM regexp_split_to_table(replace(input_content, E'\r', ''), E'\\n') AS raw(line)
    CROSS JOIN LATERAL (
      SELECT regexp_replace(
        raw.line,
        E'^\\s*(\\[[0-9]{1,3}:[0-5][0-9]([.:][0-9]{1,3})?\\])+\\s*',
        ''
      ) AS body
    ) AS parsed
    WHERE btrim(raw.line, E' \t\n\r\f\v') <> ''
      AND (
        (
          raw.line ~ E'^\\s*(\\[[0-9]{1,3}:[0-5][0-9]([.:][0-9]{1,3})?\\])+'
          AND btrim(parsed.body, E' \t\n\r\f\v') <> ''
          AND (
            parsed.body !~ E'^\\s*<[0-9]{1,3}:[0-5][0-9]([.:][0-9]{1,3})?>'
            OR regexp_replace(
              parsed.body,
              E'<[0-9]{1,3}:[0-5][0-9]([.:][0-9]{1,3})?>',
              '',
              'g'
            ) ~ E'<[^>]*(>|$)'
            OR btrim(regexp_replace(
              parsed.body,
              E'<[0-9]{1,3}:[0-5][0-9]([.:][0-9]{1,3})?>',
              '',
              'g'
            ), E' \t\n\r\f\v') = ''
            OR EXISTS (
              SELECT 1
              FROM (
                SELECT sequenced.timestamp_ms,
                  lag(sequenced.timestamp_ms) OVER (ORDER BY sequenced.position) AS previous_timestamp_ms
                FROM (
                  SELECT marker.position,
                    marker.parts[1]::integer * 60000
                    + marker.parts[2]::integer * 1000
                    + coalesce(rpad(marker.parts[4], 3, '0')::integer, 0) AS timestamp_ms
                  FROM regexp_matches(
                    parsed.body,
                    E'<([0-9]{1,3}):([0-5][0-9])([.:]([0-9]{1,3}))?>',
                    'g'
                  ) WITH ORDINALITY AS marker(parts, position)
                ) AS sequenced
              ) AS ordered
              WHERE ordered.timestamp_ms < ordered.previous_timestamp_ms
            )
          )
        )
        OR (
          raw.line !~ E'^\\s*(\\[[0-9]{1,3}:[0-5][0-9]([.:][0-9]{1,3})?\\])+'
          AND raw.line !~ E'^\\s*(\\[[A-Za-z][A-Za-z0-9_-]*:[^\\[\\]\\r\\n]*\\]\\s*)+$'
        )
      )
  ) THEN 'WORD'::lyrics_timing
  ELSE 'LINE'::lyrics_timing
END
$$;
--> statement-breakpoint
UPDATE lyrics
SET timing = xymusic_detect_lyrics_timing(format::text, content);
--> statement-breakpoint
UPDATE track_metadata
SET raw_tags = jsonb_set(
  raw_tags,
  '{lyrics,timing}',
  to_jsonb(xymusic_detect_lyrics_timing(raw_tags #>> '{lyrics,format}', raw_tags #>> '{lyrics,content}')::text),
  true
)
WHERE jsonb_typeof(raw_tags -> 'lyrics') = 'object';
--> statement-breakpoint
UPDATE track_metadata
SET overrides = jsonb_set(
  overrides,
  '{lyrics,timing}',
  to_jsonb(xymusic_detect_lyrics_timing(overrides #>> '{lyrics,format}', overrides #>> '{lyrics,content}')::text),
  true
)
WHERE jsonb_typeof(overrides -> 'lyrics') = 'object';
--> statement-breakpoint
UPDATE track_metadata_revisions
SET raw_tags = jsonb_set(
  raw_tags,
  '{lyrics,timing}',
  to_jsonb(xymusic_detect_lyrics_timing(raw_tags #>> '{lyrics,format}', raw_tags #>> '{lyrics,content}')::text),
  true
)
WHERE jsonb_typeof(raw_tags -> 'lyrics') = 'object';
--> statement-breakpoint
UPDATE track_metadata_revisions
SET overrides = jsonb_set(
  overrides,
  '{lyrics,timing}',
  to_jsonb(xymusic_detect_lyrics_timing(overrides #>> '{lyrics,format}', overrides #>> '{lyrics,content}')::text),
  true
)
WHERE jsonb_typeof(overrides -> 'lyrics') = 'object';
--> statement-breakpoint
UPDATE track_metadata_revisions
SET effective_tags = jsonb_set(
  effective_tags,
  '{lyrics,timing}',
  to_jsonb(xymusic_detect_lyrics_timing(effective_tags #>> '{lyrics,format}', effective_tags #>> '{lyrics,content}')::text),
  true
)
WHERE jsonb_typeof(effective_tags -> 'lyrics') = 'object';
--> statement-breakpoint
UPDATE metadata_writeback_jobs
SET metadata_snapshot = jsonb_set(
  metadata_snapshot,
  '{lyrics,timing}',
  to_jsonb(xymusic_detect_lyrics_timing(metadata_snapshot #>> '{lyrics,format}', metadata_snapshot #>> '{lyrics,content}')::text),
  true
)
WHERE jsonb_typeof(metadata_snapshot -> 'lyrics') = 'object';
--> statement-breakpoint
DELETE FROM idempotency_records;
--> statement-breakpoint
ALTER TABLE lyrics ALTER COLUMN timing DROP DEFAULT;
--> statement-breakpoint
DROP FUNCTION xymusic_detect_lyrics_timing(text, text);
