-- size_bytes was int (2 GiB max); maintenance video uploads can exceed that.
ALTER TABLE files ALTER COLUMN size_bytes TYPE bigint;
