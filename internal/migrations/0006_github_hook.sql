-- GitHub's id of the webhook the panel created for auto-deploy (for cleanup).
ALTER TABLE github_repos ADD COLUMN hook_id INTEGER;
