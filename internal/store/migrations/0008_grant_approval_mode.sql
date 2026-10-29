ALTER TABLE grants ADD COLUMN approval TEXT NOT NULL DEFAULT 'elicitation' CHECK (approval IN ('elicitation', 'app'));
