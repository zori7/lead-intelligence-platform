-- +goose Up
CREATE TABLE leads (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    company_name     text        NOT NULL CHECK (char_length(company_name) BETWEEN 1 AND 200),
    website          text        CHECK (char_length(website) <= 300),
    contact_name     text        CHECK (char_length(contact_name) <= 200),
    contact_email    text        CHECK (char_length(contact_email) <= 320),
    contact_title    text        CHECK (char_length(contact_title) <= 200),
    industry         text        CHECK (char_length(industry) <= 100),
    company_size     text        CHECK (company_size IN ('1-10', '11-50', '51-200', '201-1000', '1000+')),
    notes            text        CHECK (char_length(notes) <= 5000),
    status           text        NOT NULL DEFAULT 'new'
                                 CHECK (status IN ('new', 'contacted', 'qualified', 'proposal', 'won', 'lost')),
    ai_score         smallint    CHECK (ai_score BETWEEN 0 AND 100),
    ai_tier          text        CHECK (ai_tier IN ('hot', 'warm', 'cold')),
    last_analyzed_at timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX leads_status ON leads (status);
CREATE INDEX leads_created_at ON leads (created_at DESC);
CREATE INDEX leads_ai_score ON leads (ai_score DESC NULLS LAST);

-- +goose Down
DROP TABLE leads;
