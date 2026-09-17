# 官方参考资料

- Percona Toolkit / pt-archiver: https://docs.percona.com/percona-toolkit/pt-archiver.html
- Percona XtraBackup 8.0: https://docs.percona.com/percona-xtrabackup/8.0/
- Percona XtraBackup replication setup: https://docs.percona.com/percona-xtrabackup/8.4/set-up-replication.html
- MySQL Shell Utilities: https://dev.mysql.com/doc/mysql-shell/26.7/en/mysql-shell-utilities.html
- Oracle 19c Backup and Recovery User's Guide: https://docs.oracle.com/en/database/oracle/oracle-database/19/bradv/index.html
- PostgreSQL Backup and Restore: https://www.postgresql.org/docs/current/backup.html
- PostgreSQL pg_dump: https://www.postgresql.org/docs/current/app-pgdump.html
- PostgreSQL pg_basebackup: https://www.postgresql.org/docs/current/app-pgbasebackup.html
- Apache Doris Backup: https://doris.apache.org/docs/dev/admin-manual/data-admin/backup-restore/backup/
- Apache Doris Restore: https://doris.apache.org/docs/dev/admin-manual/data-admin/backup-restore/restore/

设计原则：工具版本和数据库版本的兼容关系不硬编码在业务代码中，由 `software_packages.compatibility` 与安装前 PreCheck 管理。
