-- pgvector stores and searches embeddings for the strata assistant. Like
-- citext it is installed once into public, so per-schema test databases
-- (search_path "<schema>,public") and production both resolve it.
CREATE EXTENSION IF NOT EXISTS vector SCHEMA public;
