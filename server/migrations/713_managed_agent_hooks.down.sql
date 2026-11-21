-- Hook tables can predate this migration on fork installations. Do not drop
-- user data when rolling back an idempotent reconciliation.
SELECT 1;
