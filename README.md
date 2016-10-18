# RDSLogs
[![Build Status](https://travis-ci.org/honeycombio/rdslogs.svg?branch=master)](https://travis-ci.org/honeycombio/rdslogs)

`rdslogs` is a tool to download or stream log files from RDS.

The default action of `rdslogs` is to stream the current log file. Use the
`--download` flag to download log files instead.

Outputs to STDOUT for easy piping to [Honeycomb](https://honeycomb.io) or any other CLI:
```
rdslogs --identifier my-rds-database | honeytail -p mysql -k <writekey> -d "RDS Logs" -f -
```

# Installation

```
go get github.com/honeycombio/rdslogs
```

# Usage
```
Usage:
  rdslogs --identifier my-rds-instance

rdslogs streams a log file from Amazon RDS and prints it to STDOUT.

AWS credentials are required and can be provided via IAM roles, AWS shared
config (~/.aws/config), AWS shared credentials (~/.aws/credentials), or
the environment variables AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY.

In Download mode, instead of tailing, it downloads the log file specified by the
--log_file flag (and the past 24hrs of rotated logs) to the directory specified
by the --download_dir flag.

Application Options:
      --region=       AWS region to use (default: us-east-1)
  -i, --identifier=   RDS instance identifier
  -f, --log_file=     RDS log file to retrieve (default: slowquery/mysql-slowquery.log)
  -d, --download      Download old logs instead of tailing the current log
      --download_dir= directory in to which log files are downloaded (default: ./)
  -v, --version       Output the current version and exit
      --config=       config file
      --debug         turn on debugging output

Help Options:
  -h, --help          Show this help message
```
