CREATE TABLE IF NOT EXISTS db_tab_trash (
    tabid varchar(36) PRIMARY KEY,
    workspaceid varchar(36) NOT NULL,
    name varchar(255) NOT NULL,
    seq integer NOT NULL,
    tabidx int NOT NULL,
    closedat bigint NOT NULL,
    data json NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tab_trash_workspace ON db_tab_trash (workspaceid, seq DESC);
