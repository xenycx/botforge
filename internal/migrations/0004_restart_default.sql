-- Before restart policies existed the runner restarted crashed bots. Keep that
-- behaviour for existing bots (new bots also default to on_failure in Go).
UPDATE bots SET restart_policy = 'on_failure';
