# RDSLogs

[![OSS Lifecycle](https://img.shields.io/osslifecycle/honeycombio/rdslogs)](https://github.com/honeycombio/home/blob/main/honeycomb-oss-lifecycle-and-practices.md)
[![Build Status](https://travis-ci.org/honeycombio/rdslogs.svg?branch=main)](https://travis-ci.org/honeycombio/rdslogs)

`rdslogs` is a tool to download or stream log files from RDS. When streaming, you
can choose to stream them to STDOUT or directly to Honeycomb.

To learn more about using Honeycomb, see our [docs](https://honeycomb.io/docs) (and [RDS-specific docs](https://honeycomb.io/docs/connect/mysql/rds/)).

The default action of `rdslogs` is to stream the current log file. Use the
`--download` flag to download log files instead.

The default output is STDOUT to see what's happening:

```sh
rdslogs --region us-east-1 --identifier my-rds-database
```

To output the results directly to Honeycomb, use the `--output honeycomb` flag
and include the `--writekey` and `--dataset` flags.  Optionally, the
`--sample_rate` flag will only send a portion of your traffic to Honeycomb.

```sh
rdslogs --region us-east-1 --identifier my-rds-database --output honeycomb --writekey abcabc123123 --dataset "rds logs"
```

## Deprecation Notice for MySQL, MariaDB, and Aurora

`rdslogs` is deprecated for MySQL, MariaDB, and Aurora: please use Cloudwatch Logs combined with our [Agentless Integrations for AWS](https://github.com/honeycombio/agentless-integrations-for-aws#mysql-rds-integration-for-cloudwatch-logs).

`rdslogs` relies on the RDS API to tail mysql logs in realtime. Due to a bug in the API, slow query logs can randomly "disappear" for long periods of time, leaving large gaps in your MySQL Dataset. Amazon has acknowledged the bug, but has no ETA, so we have deprecated this tool in favor of Cloudwatch Logs, which are more reliable.

## Installation

`rdslogs` is available as a `.deb` or `.rpm` package from [`honeycombio`][hq];
see the [MySQL RDS][mysql-rds-download] or [PostgreSQL RDS][pg-rds-download]
integration documentation for links and command line instructions.

[hq]: https://honeycomb.io
[mysql-rds-download]: https://honeycomb.io/docs/getting-data-in/integrations/databases/mysql/rds/#download-the-rds-connector-rdslogs
[pg-rds-download]: https://honeycomb.io/docs/getting-data-in/integrations/databases/postgresql/rds/#download-the-rds-connector-rdslogs

When installed from a package, there is a config file at
`/etc/rdslogs/rdslogs.conf`. Instead of using the command line flags as
indicated in the previous section, edit the config file with the intended
values.  After doing so, start the service with the standard `sudo initctl start
rdslogs` (upstart) or `sudo systemctl start rdslogs` (systemd) commands.

To build and install directly from source:

```sh
go get github.com/honeycombio/rdslogs
```

## Usage

```nil
$ rdslogs --help
Usage:
  rdslogs rdslogs --identifier my-rds-instance

rdslogs streams a log file from Amazon RDS and prints it to STDOUT or sends it
up to Honeycomb.io.
```

### AWS Requirements

AWS credentials are required and can be provided via IAM roles, AWS shared
config (`~/.aws/config`), AWS shared credentials (`~/.aws/credentials`), or
the environment variables `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY`.
Below is the minimal IAM policy needed by RDSLogs.

```json
{
    "Version": "2012-10-17",
    "Statement": [
    {
        "Effect": "Allow",
        "Action": [
            "rds:DescribeDBInstances",
            "rds:DescribeDBLogFiles",
            "rds:DownloadDBLogFilePortion"
        ],
        "Resource": "*"
    }
  ]
}
```

Passing `--download` triggers Download Mode, in which `rdslogs` will download the
specified logs to the directory specified by `--download_dir`. Logs are specified
via the `--log_file` flag, which names an active log file as well as the past 24
hours of rotated logs. (For example, specifying `--log_file=foo.log` will download
`foo.log` as well as `foo.log.0`, `foo.log.2`, ... `foo.log.23`.)

When `--output` is set to `honeycomb`, the `--writekey` and `--dataset` flags are
required. Instead of being printed to STDOUT, database events from the log will
be transmitted to Honeycomb. `--scrub_query` and `--sample_rate` also only apply to
Honeycomb output.

```nil
Application Options:
      --region=               AWS region to use (default: us-east-1)
  -i, --identifier=           RDS instance identifier
      --dbtype=               RDS database type. Accepted values are mysql and postgresql.
                              (default: mysql)
      --log_type=             Log file type. Accepted values are query and audit. Audit is
                              currently only supported for mysql. (default: query)
  -f, --log_file=             RDS log file to retrieve
  -d, --download              Download old logs instead of tailing the current log
      --download_dir=         directory in to which log files are downloaded (default: ./)
      --num_lines=            number of lines to request at a time from AWS. Larger number will
                              be more efficient, smaller number will allow for longer lines
                              (default: 10000)
      --backoff_timer=        how many seconds to pause when rate limited by AWS. (default: 5)
  -o, --output=               output for the logs: stdout or honeycomb (default: stdout)
      --writekey=             Team write key, when output is honeycomb
      --dataset=              Name of the dataset, when output is honeycomb
      --api_host=             Hostname for the Honeycomb API server (default:
                              https://api.honeycomb.io/)
      --scrub_query           Replaces the query field with a one-way hash of the contents
      --sample_rate=          Only send 1 / N log lines (default: 1)
  -a, --add_field=            Extra fields to send in request, in the style of "field:value"
  -v, --version               Output the current version and exit
  -c, --config=               config file
      --write_default_config  Write a default config file to STDOUT
      --debug                 turn on debugging output

Help Options:
  -h, --help                  Show this help message
```
