-- Installation identity: one row, created on first start. Containers are
-- labelled with it so panels sharing a Docker daemon never touch each other's
-- containers. A restored installation keeps its identity (it is in the database).
CREATE TABLE installation (
    id            INTEGER PRIMARY KEY NOT NULL CHECK (id = 1),
    install_id    TEXT NOT NULL CHECK (length(install_id) = 36),
    created_at_ms INTEGER NOT NULL
) STRICT;
