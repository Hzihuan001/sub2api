-- Persist the normalized single-image input used by the app-managed fan-out
-- worker. This avoids losing prompts or references when the process restarts.
ALTER TABLE batch_image_items
    ADD COLUMN IF NOT EXISTS input_payload JSONB;

