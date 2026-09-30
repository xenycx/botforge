-- Discord "webhook.incoming" grant: the webhook URL is a bearer secret, so it
-- is sealed with the keyring like OAuth tokens.
ALTER TABLE oauth_accounts ADD COLUMN webhook_ciphertext BLOB
    CHECK (webhook_ciphertext IS NULL OR length(webhook_ciphertext) >= 16);
ALTER TABLE oauth_accounts ADD COLUMN webhook_nonce BLOB
    CHECK (webhook_nonce IS NULL OR length(webhook_nonce) = 12);
ALTER TABLE oauth_accounts ADD COLUMN webhook_key_id TEXT;
