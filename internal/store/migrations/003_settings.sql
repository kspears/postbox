-- Per-account preferences, one JSON value per key. The first is "folder_order": the full names
-- of the account's folders in the order the person arranged them.
CREATE TABLE account_settings (
    account TEXT NOT NULL,
    key     TEXT NOT NULL,
    value   TEXT NOT NULL,
    PRIMARY KEY (account, key)
) STRICT;
