-- Initialize databases for dashboard integration demo
-- This script sets up both the dashboard and outpost databases with their respective users

-- Create dashboard database and user
CREATE DATABASE dashboard_integration;
CREATE USER dashboard WITH PASSWORD 'dashboard';
GRANT ALL PRIVILEGES ON DATABASE dashboard_integration TO dashboard;

-- Create outpost database and user  
CREATE DATABASE outpost;
CREATE USER outpost WITH PASSWORD 'outpost';
GRANT ALL PRIVILEGES ON DATABASE outpost TO outpost;

-- Connect to dashboard database to set up schema permissions
\c dashboard_integration;
GRANT ALL ON SCHEMA public TO dashboard;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO dashboard;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO dashboard;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO dashboard;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO dashboard;

-- Create dashboard tables (Auth.js + custom tables)
CREATE TABLE verification_token
(
  identifier TEXT NOT NULL,
  expires TIMESTAMPTZ NOT NULL,
  token TEXT NOT NULL,
 
  PRIMARY KEY (identifier, token)
);
 
CREATE TABLE accounts
(
  id SERIAL,
  "userId" INTEGER NOT NULL,
  type VARCHAR(255) NOT NULL,
  provider VARCHAR(255) NOT NULL,
  "providerAccountId" VARCHAR(255) NOT NULL,
  refresh_token TEXT,
  access_token TEXT,
  expires_at BIGINT,
  id_token TEXT,
  scope TEXT,
  session_state TEXT,
  token_type TEXT,
 
  PRIMARY KEY (id)
);
 
CREATE TABLE sessions
(
  id SERIAL,
  "userId" INTEGER NOT NULL,
  expires TIMESTAMPTZ NOT NULL,
  "sessionToken" VARCHAR(255) NOT NULL,
 
  PRIMARY KEY (id)
);
 
CREATE TABLE users
(
  id SERIAL,
  name VARCHAR(255),
  email VARCHAR(255) NOT NULL UNIQUE,
  "emailVerified" TIMESTAMPTZ,
  image TEXT,
  hashed_password TEXT,
  "createdAt" TIMESTAMPTZ DEFAULT NOW(),
  "updatedAt" TIMESTAMPTZ DEFAULT NOW(),
 
  PRIMARY KEY (id)
);

-- Create index on email for faster lookups
CREATE INDEX idx_users_email ON users (email);

-- Connect to outpost database to set up schema permissions
\c outpost;
GRANT ALL ON SCHEMA public TO outpost;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO outpost;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO outpost;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO outpost;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO outpost;