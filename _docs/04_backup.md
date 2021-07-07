# Backup

For the single Dockerfile setup:

- All defined `VOLUME` section in Dockerfile will be added to the archive.

For docker-compose setup:

- All non-external, local (driver `local` or empty) volumes defined in `volumes:` section in full notation will be added
  to archive.

Backup interval defined by `-I,--backup-interval,$BACKUP_INTERVAL` and by default equal to `1h` (every 1 hour).

The default encryption is symmetric AES-256 CBC done by OpenSSL. Encryption key defined in `--backup-key,-K,$BACKUP_KEY` and
by-default equal to `git-pipe-change-me`.

Restore will be done **automatically** before the first run.

## Supported destination

Defined by `-B,--backup,$BACKUP`. Default is `file://backups`

* `file://<directory>` - archive in directory. Creates temp (`.!tmp` suffix) during backup.
* `s3://<id>:<secret>@<endpoint>/<bucket>[?params]` - upload/download to/from S3-like storage
* `<empty>` or `none` - disable backup

S3 query params:

The bucket should be created by an administrator.

* `force_path=true|false`, default `false` - force use path style for buckets. Required for Minio
* `region=<name>`, default `us-west-1` - region
* `disable_ssl=true|false`, default `false`, disable SSL for endpoint

**Example for local Minio**:

Launch minio: `docker run -p 9000:9000 minio/minio server /data`

Backup URL: `s3://minioadmin:minioadmin@127.0.0.1:9000/backups?force_path=true&disable_ssl=true`

**Example for BackBlaze (B2)**:

* Obtain id and secret from [admin panel](https://secure.backblaze.com/app_keys.htm)
* Copy information about region and bucket name [in dashboard](https://secure.backblaze.com/b2_buckets.htm)

Backup URL: `s3://<id>:<secret>@s3.<region>.backblazeb2.com/<bucket name>`

> (B2) There is some lag between backup and availability to download. Usually, it's around 2-5 minutes for me. 
