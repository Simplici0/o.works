-- +goose Up
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS materials (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    cost_per_kg NUMERIC NOT NULL,
    notes TEXT,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_materials_name ON materials(name);
CREATE INDEX IF NOT EXISTS idx_materials_active ON materials(active);

CREATE TABLE IF NOT EXISTS rate_config (
    id INTEGER PRIMARY KEY,
    machine_hourly_rate NUMERIC NOT NULL,
    labor_per_minute NUMERIC NOT NULL,
    overhead_fixed NUMERIC NOT NULL,
    overhead_percent NUMERIC NOT NULL,
    failure_rate_percent NUMERIC NOT NULL,
    tax_percent NUMERIC NOT NULL,
    currency TEXT NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_rate_config_singleton CHECK (id = 1)
);

CREATE TABLE IF NOT EXISTS shipping_rates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    scope TEXT NOT NULL,
    country TEXT NOT NULL,
    city TEXT,
    flat_cost NUMERIC NOT NULL,
    notes TEXT,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_shipping_rates_scope_country_city ON shipping_rates(scope, country, city);
CREATE INDEX IF NOT EXISTS idx_shipping_rates_active ON shipping_rates(active);

CREATE TABLE IF NOT EXISTS packaging_rates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    flat_cost NUMERIC NOT NULL,
    notes TEXT,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_packaging_rates_name ON packaging_rates(name);
CREATE INDEX IF NOT EXISTS idx_packaging_rates_active ON packaging_rates(active);

CREATE TABLE IF NOT EXISTS quotes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    title TEXT,
    notes TEXT,
    waste_percent NUMERIC NOT NULL,
    margin_percent NUMERIC NOT NULL,
    tax_enabled BOOLEAN NOT NULL,
    tax_percent_snapshot NUMERIC NOT NULL,
    totals_json TEXT NOT NULL,
    breakdown_json TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_quotes_created_at ON quotes(created_at);

CREATE TABLE IF NOT EXISTS quote_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    quote_id INTEGER NOT NULL,
    material_id INTEGER NOT NULL,
    grams NUMERIC NOT NULL,
    print_minutes NUMERIC NOT NULL,
    labor_minutes NUMERIC NOT NULL,
    quantity INTEGER NOT NULL,
    FOREIGN KEY (quote_id) REFERENCES quotes(id) ON DELETE CASCADE,
    FOREIGN KEY (material_id) REFERENCES materials(id)
);

CREATE INDEX IF NOT EXISTS idx_quote_items_quote_id ON quote_items(quote_id);
CREATE INDEX IF NOT EXISTS idx_quote_items_material_id ON quote_items(material_id);

-- +goose Down
DROP TABLE IF EXISTS quote_items;
DROP TABLE IF EXISTS quotes;
DROP TABLE IF EXISTS packaging_rates;
DROP TABLE IF EXISTS shipping_rates;
DROP TABLE IF EXISTS rate_config;
DROP TABLE IF EXISTS materials;
DROP TABLE IF EXISTS users;
