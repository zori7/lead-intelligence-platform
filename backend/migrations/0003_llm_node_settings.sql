-- +goose Up
-- Exactly five rows, seeded here; the API only ever updates them (full replace).
-- Limits mirror ModelName and Temperature in api/openapi.yaml.
CREATE TABLE llm_node_settings (
    node        text         PRIMARY KEY
                             CHECK (node IN ('default', 'extract_company', 'analyze_fit',
                                             'analyze_intent', 'generate_recommendations')),
    model       text         CHECK (char_length(model) BETWEEN 1 AND 200),
    temperature numeric(3,2) CHECK (temperature BETWEEN 0 AND 2),
    updated_at  timestamptz  NOT NULL DEFAULT now()
);

INSERT INTO llm_node_settings (node) VALUES
    ('default'), ('extract_company'), ('analyze_fit'), ('analyze_intent'), ('generate_recommendations');

-- +goose Down
DROP TABLE llm_node_settings;
