ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS working_directory VARCHAR(1024);

COMMENT ON COLUMN usage_logs.working_directory IS 'Client working directory extracted from supported CLI request context';
