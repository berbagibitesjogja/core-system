-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION trigger_normalize_sponsor_phone()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.phone IS NOT NULL AND NEW.phone != '' THEN
        NEW.phone := normalize_phone_number(NEW.phone);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_normalize_sponsor_phone
BEFORE INSERT OR UPDATE OF phone ON sponsors
FOR EACH ROW
EXECUTE FUNCTION trigger_normalize_sponsor_phone();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS trg_normalize_sponsor_phone ON sponsors;
DROP FUNCTION IF EXISTS trigger_normalize_sponsor_phone();
-- +goose StatementEnd
