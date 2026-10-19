-- Runs once on first initialisation of the db_auth data directory
-- (POSTGRES_DB=edupilot_auth_db is created by the image itself).
-- Idempotent: each CREATE DATABASE only runs if the database is missing.
SELECT 'CREATE DATABASE edupilot_instantion_db'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'edupilot_instantion_db')\gexec

SELECT 'CREATE DATABASE edupilot_gateway_db'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'edupilot_gateway_db')\gexec

SELECT 'CREATE DATABASE edupilot_assesment_db'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'edupilot_assesment_db')\gexec

