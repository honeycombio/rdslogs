# RDSLogs
[![Build Status](https://travis-ci.org/honeycombio/rdslogs.svg?branch=master)](https://travis-ci.org/honeycombio/rdslogs)

`rdslogs` is a tool to download or stream log files from RDS. When streaming, you
can choose to stream them to STDOUT or directly to Honeycomb.

The default action of `rdslogs` is to stream the current log file. Use the
`--download` flag to download log files instead.

The default output is STDOUT to see what's happening:
```
rdslogs --region us-east-1 --identifier my-rds-database
```

To output the results directly to Honeycomb, use the `--output honeycomb` flag and include the `--writekey` and `--dataset` flags.  Optionally, the `--sample_rate` flag will only send a portion of your traffic to Honeycomb.
```
rdslogs --region us-east-1 --identifier my-rds-database --output honeycomb --writekey abcabc123123 --dataset "rds logs"
```

# Installation

```
go get github.com/honeycombio/rdslogs
```

# Usage
➜  ./rdslogs --help
Usage:
  rdslogs rdslogs --identifier my-rds-instance

rdslogs streams a log file from Amazon RDS and prints it to STDOUT or sends it
up to Honeycomb.io.

AWS credentials are required and can be provided via IAM roles, AWS shared
config (~/.aws/config), AWS shared credentials (~/.aws/credentials), or
the environment variables AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY.

Passing --download triggers Download Mode, in which rdslogs will download the
specified logs to the directory specified by --download_dir. Logs are specified
via the --log_file flag, which names an active log file as well as the past 24
hours of rotated logs. (For example, specifying --log_file=foo.log will download
foo.log as well as foo.log.0, foo.log.2, ... foo.log.23.)

When --output is set to "honeycomb", the --writekey and --dataset flags are
required. Instead of being printed to STDOUT, database events from the log will
be transmitted to Honeycomb. --scrub_query and --sample_rate also only apply to
honeycomb output.


Application Options:
      --region=       AWS region to use (default: us-east-1)
  -i, --identifier=   RDS instance identifier
  -f, --log_file=     RDS log file to retrieve (default: slowquery/mysql-slowquery.log)
  -d, --download      Download old logs instead of tailing the current log
      --download_dir= directory in to which log files are downloaded (default: ./)
      --num_lines=    number of lines to request at a time from AWS. Larger number will be more efficient, smaller number will allow for longer lines (default: 1000)
  -o, --output=       output for the logs: stdout or honeycomb (default: stdout)
      --writekey=     Team write key, when output is honeycomb
      --dataset=      Name of the dataset, when output is honeycomb
      --api_host=     Hostname for the Honeycomb API server (default: https://api.honeycomb.io/)
      --scrub_query   Replaces the query field with a one-way hash of the contents
      --sample_rate=  Only send 1 / N log lines (default: 1)
  -v, --version       Output the current version and exit
      --config=       config file
      --debug         turn on debugging output

Help Options:
  -h, --help          Show this help message
```
