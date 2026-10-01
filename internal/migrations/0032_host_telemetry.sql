-- More detail per host sample for the Host page: load average, swap, network
-- and disk throughput. Existing rows read as zero.
ALTER TABLE node_telemetry ADD COLUMN load1 REAL NOT NULL DEFAULT 0 CHECK (load1 >= 0);
ALTER TABLE node_telemetry ADD COLUMN swap_used_bytes INTEGER NOT NULL DEFAULT 0 CHECK (swap_used_bytes >= 0);
ALTER TABLE node_telemetry ADD COLUMN swap_total_bytes INTEGER NOT NULL DEFAULT 0 CHECK (swap_total_bytes >= 0);
ALTER TABLE node_telemetry ADD COLUMN net_rx_bps INTEGER NOT NULL DEFAULT 0 CHECK (net_rx_bps >= 0);
ALTER TABLE node_telemetry ADD COLUMN net_tx_bps INTEGER NOT NULL DEFAULT 0 CHECK (net_tx_bps >= 0);
ALTER TABLE node_telemetry ADD COLUMN disk_read_bps INTEGER NOT NULL DEFAULT 0 CHECK (disk_read_bps >= 0);
ALTER TABLE node_telemetry ADD COLUMN disk_write_bps INTEGER NOT NULL DEFAULT 0 CHECK (disk_write_bps >= 0);
