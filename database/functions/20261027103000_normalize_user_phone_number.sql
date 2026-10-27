-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION normalize_phone_number(raw_phone TEXT, default_cc TEXT DEFAULT '62')
RETURNS TEXT AS $$
DECLARE
    cleaned TEXT;
BEGIN
    IF raw_phone IS NULL THEN
        RETURN NULL;
    END IF;

    -- 1. Strip all non-digit characters (spaces, dashes, plus signs, parentheses, etc.)
    cleaned := regexp_replace(raw_phone, '[^0-9]', '', 'g');

    IF cleaned = '' THEN
        RETURN '';
    END IF;

    -- 2. Strip leading international dialing prefix "00" (e.g. 0062... -> 62...)
    IF cleaned LIKE '00%' THEN
        cleaned := substring(cleaned from 3);
    END IF;

    -- 3. If starts with CC followed by a redundant local zero (e.g. +62 0801 -> 620801 -> 62801)
    IF cleaned LIKE default_cc || '0%' THEN
        cleaned := default_cc || substring(cleaned from length(default_cc) + 2);
    -- 4. If starts with standard local zero (e.g. 0801 -> 62801)
    ELSIF cleaned LIKE '0%' THEN
        cleaned := default_cc || substring(cleaned from 2);
    -- 5. If already starts with CC and valid subscriber number (e.g. 62801)
    ELSIF cleaned LIKE default_cc || '%' THEN
        NULL;
    -- 6. If raw subscriber number without CC or leading zero (e.g. 801 -> 62801)
    ELSE
        cleaned := default_cc || cleaned;
    END IF;

    RETURN cleaned;
END;
$$ LANGUAGE plpgsql IMMUTABLE STRICT;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION trigger_normalize_user_phone_number()
RETURNS TRIGGER AS $$
BEGIN
    NEW.phone_number := normalize_phone_number(NEW.phone_number);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_normalize_user_phone_number
BEFORE INSERT OR UPDATE OF phone_number ON users
FOR EACH ROW
EXECUTE FUNCTION trigger_normalize_user_phone_number();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS trg_normalize_user_phone_number ON users;
DROP FUNCTION IF EXISTS trigger_normalize_user_phone_number();
DROP FUNCTION IF EXISTS normalize_phone_number(TEXT, TEXT);
-- +goose StatementEnd
