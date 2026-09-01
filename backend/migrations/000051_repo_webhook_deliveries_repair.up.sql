-- Some long-lived development/production databases recorded the original
-- Repo migration while missing its webhook delivery relation. Reconcile the
-- append-only delivery ledger without touching repositories or Git data.
CREATE TABLE IF NOT EXISTS repo_webhook_deliveries (
    provider TEXT NOT NULL CHECK (provider = 'github'),
    delivery_id TEXT NOT NULL CHECK (length(delivery_id) > 0),
    repository_id UUID NOT NULL
        REFERENCES repo_repositories(repository_id) ON DELETE CASCADE,
    event_name TEXT NOT NULL CHECK (length(event_name) > 0),
    ref_name TEXT,
    before_sha TEXT
        CHECK (before_sha IS NULL OR before_sha ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'),
    after_sha TEXT
        CHECK (after_sha IS NULL OR after_sha ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'),
    payload_sha256 TEXT NOT NULL CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
    status TEXT NOT NULL
        CHECK (status IN ('accepted', 'ignored', 'processed', 'failed')),
    error_code TEXT,
    received_at TIMESTAMPTZ NOT NULL,
    processed_at TIMESTAMPTZ,
    PRIMARY KEY (provider, delivery_id)
);

-- CREATE TABLE IF NOT EXISTS cannot repair a relation left partially created
-- by an old development deployment. Adding a required column is safe for an
-- empty partial ledger; PostgreSQL deliberately rejects it when rows already
-- exist and their required values cannot be inferred.
ALTER TABLE repo_webhook_deliveries
    ADD COLUMN IF NOT EXISTS provider TEXT NOT NULL CHECK (provider = 'github'),
    ADD COLUMN IF NOT EXISTS delivery_id TEXT NOT NULL CHECK (length(delivery_id) > 0),
    ADD COLUMN IF NOT EXISTS repository_id UUID NOT NULL
        REFERENCES repo_repositories(repository_id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS event_name TEXT NOT NULL CHECK (length(event_name) > 0),
    ADD COLUMN IF NOT EXISTS ref_name TEXT,
    ADD COLUMN IF NOT EXISTS before_sha TEXT
        CHECK (before_sha IS NULL OR before_sha ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'),
    ADD COLUMN IF NOT EXISTS after_sha TEXT
        CHECK (after_sha IS NULL OR after_sha ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'),
    ADD COLUMN IF NOT EXISTS payload_sha256 TEXT NOT NULL
        CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
    ADD COLUMN IF NOT EXISTS status TEXT NOT NULL
        CHECK (status IN ('accepted', 'ignored', 'processed', 'failed')),
    ADD COLUMN IF NOT EXISTS error_code TEXT,
    ADD COLUMN IF NOT EXISTS received_at TIMESTAMPTZ NOT NULL,
    ADD COLUMN IF NOT EXISTS processed_at TIMESTAMPTZ;

ALTER TABLE repo_webhook_deliveries
    ALTER COLUMN provider SET NOT NULL,
    ALTER COLUMN delivery_id SET NOT NULL,
    ALTER COLUMN repository_id SET NOT NULL,
    ALTER COLUMN event_name SET NOT NULL,
    ALTER COLUMN payload_sha256 SET NOT NULL,
    ALTER COLUMN status SET NOT NULL,
    ALTER COLUMN received_at SET NOT NULL;

DO $$
DECLARE
    provider_attribute SMALLINT;
    delivery_attribute SMALLINT;
BEGIN
    SELECT attnum INTO provider_attribute
    FROM pg_attribute
    WHERE attrelid = 'repo_webhook_deliveries'::regclass
      AND attname = 'provider'
      AND NOT attisdropped;

    SELECT attnum INTO delivery_attribute
    FROM pg_attribute
    WHERE attrelid = 'repo_webhook_deliveries'::regclass
      AND attname = 'delivery_id'
      AND NOT attisdropped;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'repo_webhook_deliveries'::regclass
          AND contype IN ('p', 'u')
          AND conkey = ARRAY[provider_attribute, delivery_attribute]::SMALLINT[]
    ) THEN
        ALTER TABLE repo_webhook_deliveries
            ADD CONSTRAINT repo_webhook_deliveries_provider_delivery_unique
            UNIQUE (provider, delivery_id);
    END IF;
END
$$;

CREATE INDEX IF NOT EXISTS repo_webhook_deliveries_repository_idx
    ON repo_webhook_deliveries (repository_id, received_at DESC);
