# Plan

1. Replace extracted prompt records with complete upstream-attempt records.
2. Capture shared HTTP transport plus the OpenAI WS exception path.
3. Persist zstd `BYTEA` payloads in Shanghai-day PostgreSQL partitions.
4. Archive, verify, analyze, and drop one closed partition daily.
5. Verify locally and stop for user review; do not commit, push, create a PR, or deploy.
