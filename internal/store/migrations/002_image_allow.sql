-- Senders whose remote images an account always loads ("Always load from this sender"). The
-- sender is the exact From address, lower-cased.
CREATE TABLE image_allow (
    account TEXT NOT NULL,
    sender  TEXT NOT NULL,
    PRIMARY KEY (account, sender)
) STRICT;
