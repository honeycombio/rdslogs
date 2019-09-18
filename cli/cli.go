package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/rds"
	"github.com/honeycombio/honeytail/parsers"
	"github.com/honeycombio/honeytail/parsers/csv"
	"github.com/honeycombio/honeytail/parsers/mysql"
	"github.com/honeycombio/honeytail/parsers/postgresql"
	"github.com/honeycombio/rdslogs/publisher"
	"github.com/sirupsen/logrus"
)

// Fortunately for us, the RDS team has diligently ignored requests to make
// RDS Postgres's `log_line_prefix` customizable for years
// (https://forums.aws.amazon.com/thread.jspa?threadID=143460).
// So we can hard-code this prefix format for Postgres log lines.
const rdsPostgresLinePrefix = "%t:%r:%u@%d:[%p]:"

const DBTypePostgreSQL = "postgresql"
const DBTypeMySQL = "mysql"

const LogTypeQuery = "query"
const LogTypeAudit = "audit"

// Options contains all the CLI flags
type Options struct {
	Region             string            `long:"region" description:"AWS region to use" default:"us-east-1"`
	InstanceIdentifier string            `short:"i" long:"identifier" description:"RDS instance identifier"`
	DBType             string            `long:"dbtype" description:"RDS database type. Accepted values are mysql and postgresql." default:"mysql"`
	LogType            string            `long:"log_type" description:"Log file type. Accepted values are query and audit. Audit is currently only supported for mysql." default:"query"`
	LogFile            string            `short:"f" long:"log_file" description:"RDS log file to retrieve"`
	Download           bool              `short:"d" long:"download" description:"Download old logs instead of tailing the current log"`
	DownloadDir        string            `long:"download_dir" description:"directory in to which log files are downloaded" default:"./"`
	NumLines           int64             `long:"num_lines" description:"number of lines to request at a time from AWS. Larger number will be more efficient, smaller number will allow for longer lines" default:"10000"`
	BackoffTimer       int64             `long:"backoff_timer" description:"how many seconds to pause when rate limited by AWS." default:"5"`
	Output             string            `short:"o" long:"output" description:"output for the logs: stdout or honeycomb" default:"stdout"`
	WriteKey           string            `long:"writekey" description:"Team write key, when output is honeycomb"`
	Dataset            string            `long:"dataset" description:"Name of the dataset, when output is honeycomb"`
	APIHost            string            `long:"api_host" description:"Hostname for the Honeycomb API server" default:"https://api.honeycomb.io/"`
	ScrubQuery         bool              `long:"scrub_query" description:"Replaces the query field with a one-way hash of the contents"`
	SampleRate         int               `long:"sample_rate" description:"Only send 1 / N log lines" default:"1"`
	AddFields          map[string]string `short:"a" long:"add_field" description:"Extra fields to send in request, in the style of \"field:value\""`

	Version            bool   `short:"v" long:"version" description:"Output the current version and exit"`
	ConfigFile         string `short:"c" long:"config" description:"config file" no-ini:"true"`
	WriteDefaultConfig bool   `long:"write_default_config" description:"Write a default config file to STDOUT" no-ini:"true"`
	Debug              bool   `long:"debug" description:"turn on debugging output"`
}

// Usage info for --help
var Usage = `rdslogs --identifier my-rds-instance

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
`

// CLI contains handles to the provided Options + aws.RDS struct
type CLI struct {
	// Options is for command line options
	Options *Options
	// RDS is an initialized session connected to RDS
	RDS *rds.RDS
	// Abort carries a true message when we catch CTRL-C so we can clean up
	Abort chan bool

	// target to which to send output
	output publisher.Publisher
	// allow changing the time for tests
	fakeNower Nower
}

// Stream polls the RDS log endpoint forever to effectively tail the logs and
// spits them out to either stdout or to Honeycomb.
func (c *CLI) Stream() error {
	// make sure we have a valid log file from which to stream
	latestFile, err := c.GetLatestLogFile()
	if err != nil {
		return err
	}
	// create the chosen output publisher target
	if c.Options.Output == "stdout" {
		c.output = &publisher.STDOUTPublisher{}
	} else {
		var parser parsers.Parser
		if c.Options.DBType == DBTypeMySQL && c.Options.LogType == LogTypeQuery {
			parser = &mysql.Parser{}
			parser.Init(&mysql.Options{})
		} else if c.Options.DBType == DBTypeMySQL && c.Options.LogType == LogTypeAudit {
			parser = &csv.Parser{}
			parser.Init(&csv.Options{
				Fields:          "time,hostname,user,source_addr,connection_id,query_id,event_type,database,query,error_code",
				TimeFieldName:   "time",
				TimeFieldFormat: "20060102 15:04:05",
			})
		} else if c.Options.DBType == DBTypePostgreSQL {
			parser = &postgresql.Parser{}
			parser.Init(&postgresql.Options{LogLinePrefix: rdsPostgresLinePrefix})
		} else {
			return fmt.Errorf(
				"Unsupported (dbtype, log_type) pair (`%s`,`%s`)",
				c.Options.DBType, c.Options.LogType)
		}

		pub := &publisher.HoneycombPublisher{
			Writekey:   c.Options.WriteKey,
			Dataset:    c.Options.Dataset,
			APIHost:    c.Options.APIHost,
			ScrubQuery: c.Options.ScrubQuery,
			SampleRate: c.Options.SampleRate,
			AddFields:  c.Options.AddFields,
			Parser:     parser,
		}
		defer pub.Close()
		c.output = pub
	}

	// forever, download the most recent entries
	sPos := StreamPos{
		logFile: LogFile{LogFileName: latestFile.LogFileName},
	}
	// for mysql audit logs, we always want the first logfile, which may not
	// show up in GetLatestLogFiles if rdslogs started mid-rotation
	if c.Options.DBType == DBTypeMySQL && c.Options.LogType == LogTypeAudit {
		sPos.logFile.LogFileName = c.Options.LogFile
	}
	for {
		// check for signal triggered exit
		select {
		case <-c.Abort:
			return fmt.Errorf("signal triggered exit")
		default:
		}

		// get recent log entries
		resp, err := c.getRecentEntries(sPos)
		if err != nil {
			if strings.HasPrefix(err.Error(), "Throttling: Rate exceeded") {
				logrus.Infof("AWS Rate limit hit; sleeping for %d seconds.\n", c.Options.BackoffTimer)
				c.waitFor(time.Duration(c.Options.BackoffTimer) * time.Second)
				continue
			}
			if strings.HasPrefix(err.Error(), "InvalidParameterValue: This file contains binary data") {
				logrus.Infof("binary data at marker %s, skipping 1000 in marker position\n", sPos.marker)
				// skip over inaccessible data
				newMarker, err := sPos.Add(1000)
				if err != nil {
					return err
				}
				sPos.marker = newMarker
				continue
			}
			if strings.HasPrefix(err.Error(), "DBLogFileNotFoundFault") {
				logrus.WithError(err).
					Warn("log does not appear to exist (rotation ongoing?) - waiting and retrying")
				c.waitFor(time.Second * 5)
				continue
			}
			return err
		}
		if resp.LogFileData != nil {
			c.output.Write(*resp.LogFileData)
		}
		if c.Options.DBType == DBTypeMySQL && c.Options.LogType == LogTypeAudit {
			// The MariaDB audit plugin rotates based on size, not time. If no data
			// is being returned, it may have been rotated, or maybe the db is just
			// very quiet and nothing is being logged. We'll have to inspect
			// the log sizes to be sure

			// If we reset our marker, asked for logs, and got an empty marker back,
			// we don't have anything to do but wait
			if sPos.marker == "0" && (resp.Marker != nil && *resp.Marker == "") {
				c.waitFor(time.Second * 5)
				continue
			}

			// Two scenarios can occur during log rotation, depending on timing
			// - the file doesn't exist because rotation is ongoing, in which case
			// AdditionalDataPending will be false, marker will be "", and LogFileData will be nil
			// - the file exists but it's been rotated, meaning our marker is wrong and doesn't point
			// at a valid position - in this case, RDS will return the marker back to us with ""
			// for logfile data
			// In either scenario, we need to check for a new file. When we're sure there's a new file,
			// reset the marker
			if (resp.Marker != nil && resp.LogFileData != nil && sPos.marker == *resp.Marker) ||
				!*resp.AdditionalDataPending && resp.LogFileData == nil {
				newestFile, err := c.GetLatestLogFile()
				if err != nil {
					return err
				}

				// If the latest log file doesn't match the first log file (i.e
				// server_audit.log.1 exists but not server_audit.log) we're in the
				// middle of a rotation, so let's wait
				if newestFile.LogFileName != sPos.logFile.LogFileName {
					logrus.WithFields(logrus.Fields{
						"expectedFile": sPos.logFile.LogFileName,
						"newestFile":   newestFile.LogFileName,
					}).Info("newest file is a rotated file, we appear to be mid-rotation")
					c.waitFor(time.Second * 5)
					continue
				}

				// ok there's a server_audit.log file out there
				// check the current position of the last read (this appears to be in bytes)
				splitMarker := strings.Split(sPos.marker, ":")
				if len(splitMarker) != 2 {
					// something's wrong. marker should have been #:#
					logrus.WithField("marker", sPos.marker).
						Warn("marker didn't split into two pieces across a colon")
					continue
				}
				offset, _ := strconv.Atoi(splitMarker[1])

				// if our last position is greater in size than the current file
				// a rotation has probably occurred and we can reset the marker
				if int64(offset) > newestFile.Size {
					logrus.WithFields(logrus.Fields{
						"currentOffset": offset,
						"newFileSize":   newestFile.Size,
					}).Debug("last marker offset exceeds newest file size, resetting marker to 0")
					sPos.marker = "0"
					continue
				}
			}
		}

		if !*resp.AdditionalDataPending || (resp.Marker != nil && *resp.Marker == "0") {
			if c.Options.DBType == DBTypePostgreSQL {
				// If that's all we've got for now, see if there's a newer file to
				// start tailing. This logic is only relevant for postgres: the
				// newest postgres log file will be named
				// error/postgresql.log.YYYY-MM-DD,
				// but the newest mysql log
				// will always be named
				// slowquery/mysql-slowquery.log.
				newestFile, err := c.GetLatestLogFile()
				if err != nil {
					return err
				}
				if newestFile.LogFileName != sPos.logFile.LogFileName {
					logrus.WithFields(logrus.Fields{
						"oldFile": sPos.logFile.LogFileName,
						"newFile": newestFile.LogFileName}).Debug("Found newer file")
					sPos = StreamPos{logFile: LogFile{LogFileName: newestFile.LogFileName}}
					continue
				}
			}
			// Wait for a few seconds and try again.
			c.waitFor(5 * time.Second)
		}
		newMarker := c.getNextMarker(sPos, resp)
		logrus.WithFields(logrus.Fields{
			"prevMarker": sPos.marker,
			"newMarker":  newMarker,
			"file":       sPos.logFile.LogFileName}).Debug("Got new marker")
		sPos.marker = newMarker
	}
}

// getNextMarker takes in to account the current and next reported markers and
// decides whether to believe the resp.Marker or calculate its own next marker.
func (c *CLI) getNextMarker(sPos StreamPos, resp *rds.DownloadDBLogFilePortionOutput) string {
	// if resp is nil, we're up a creek and should return sPos' marker, but at
	// least we shouldn't try and dereference it and panic.
	if resp == nil {
		logrus.Warn("resp was nil, returning previous marker")
		return sPos.marker
	}
	if resp.Marker == nil {
		logrus.Warn("resp marker is nil, returning previous marker")
		return sPos.marker
	}

	// when we get to the end of a log segment, the marker in resp is "0".
	// if it's not "0", we should trust it's correct and use it.
	if *resp.Marker != "0" {
		return *resp.Marker
	}
	// ok, we've hit the end of a segment, but did we get any data? If we got
	// data, then it's not really the end of the segment and we should calculate a
	// new marker and use that.
	if resp.LogFileData != nil && len(*resp.LogFileData) != 0 {
		newMarkerStr, err := sPos.Add(len(*resp.LogFileData))
		if err != nil {
			logrus.WithError(err).
				Warn("failed to get next marker. Reverting to no marker.")
			return "0"
		}
		return newMarkerStr
	}
	// we hit the end of a segment but we didn't get any data. we should try again
	// during the 00-05 minutes past the hour time, and roll over once we get to 6
	// minutes past the hour
	var now time.Time
	if c.fakeNower != nil {
		now = c.fakeNower.Now().UTC()
	} else {
		now = time.Now().UTC()
	}
	curMin, _ := strconv.Atoi(now.Format("04"))
	if curMin > 5 {
		logrus.WithField("newMarker", *resp.Marker).
			Debug("no log data received but it's %d minutes (> 5) past " +
				"the hour, returning resp marker")
		return *resp.Marker
	}
	logrus.WithField("prevMarker", sPos.marker).
		Debug("no log data received but it's %d minutes (< 5) past " +
			"the hour, returning previous marker")
	// let's try again from where we did the last time.
	return sPos.marker
}

// StreamPos represents a log file and marker combination
type StreamPos struct {
	logFile LogFile
	marker  string
}

// Add returns a new marker string that is the current marker + dataLen offset
func (s *StreamPos) Add(dataLen int) (string, error) {
	splitMarker := strings.Split(s.marker, ":")
	if len(splitMarker) != 2 {
		// something's wrong. marker should have been #:#
		// TODO provide a better value
		return "", fmt.Errorf("marker didn't split into two pieces across a colon")
	}
	mHour, _ := strconv.Atoi(splitMarker[0])
	mOffset, _ := strconv.Atoi(splitMarker[1])
	mOffset += dataLen
	return fmt.Sprintf("%d:%d", mHour, mOffset), nil
}

// getRecentEntries fetches the most recent lines from the log file, starting
// from marker or the end of the file if marker is nil
// returns the downloaded data
func (c *CLI) getRecentEntries(sPos StreamPos) (*rds.DownloadDBLogFilePortionOutput, error) {
	params := &rds.DownloadDBLogFilePortionInput{
		DBInstanceIdentifier: aws.String(c.Options.InstanceIdentifier),
		LogFileName:          aws.String(sPos.logFile.LogFileName),
		NumberOfLines:        aws.Int64(c.Options.NumLines),
	}
	// if we have a marker, download from there. otherwise get the most recent line
	if sPos.marker != "" {
		params.Marker = &sPos.marker
	} else {
		params.NumberOfLines = aws.Int64(1)
	}
	return c.RDS.DownloadDBLogFilePortion(params)
}

// Download downloads RDS logs and reads them all in
func (c *CLI) Download() error {
	// get a list of RDS instances, return the one to use.
	// if one's user supplied, verify it exists.
	// if not user supplied and there's only one, use that
	// else ask
	logFiles, err := c.GetLogFiles()
	if err != nil {
		return err
	}

	logFiles, err = c.DownloadLogFiles(logFiles)
	if err != nil {
		fmt.Println("Error downloading log files:")
		return err
	}

	return nil
}

// LogFile wraps the returned structure from AWS
// "Size": 2196,
// "LogFileName": "slowquery/mysql-slowquery.log.7",
// "LastWritten": 1474959300000
type LogFile struct {
	Size            int64 // in bytes?
	LogFileName     string
	LastWritten     int64 // arrives as msec since epoch
	LastWrittenTime time.Time
	Path            string
}

func (l *LogFile) String() string {
	return fmt.Sprintf("%-35s (date: %s, size: %d)", l.LogFileName, l.LastWrittenTime, l.Size)
}

// DownloadLogFiles returns a new copy of the logFile list because it mutates the contents.
func (c *CLI) DownloadLogFiles(logFiles []LogFile) ([]LogFile, error) {
	logrus.Infof("Downloading log files to %s\n", c.Options.DownloadDir)
	downloadedLogFiles := make([]LogFile, 0, len(logFiles))
	for i := range logFiles {
		// returned logFile has a modified Path
		logFile, err := c.downloadFile(logFiles[i])
		if err != nil {
			return nil, err
		}
		downloadedLogFiles = append(downloadedLogFiles, logFile)
	}
	return downloadedLogFiles, nil
}

// downloadFile fetches an individual log file. Note that AWS's RDS
// DownloadDBLogFilePortion only returns 1MB at a time, and we have to manually
// paginate it ourselves.
func (c *CLI) downloadFile(logFile LogFile) (LogFile, error) {
	// open the out file for writing
	logFile.Path = path.Join(c.Options.DownloadDir, path.Base(logFile.LogFileName))
	fmt.Printf("Downloading %s to %s ... ", logFile.LogFileName, logFile.Path)
	defer fmt.Printf("done\n")
	if err := os.MkdirAll(path.Dir(logFile.Path), os.ModePerm); err != nil {
		return logFile, err
	}
	outfile, err := os.Create(logFile.Path)
	if err != nil {
		return logFile, err
	}
	defer outfile.Close()

	resp := &rds.DownloadDBLogFilePortionOutput{
		AdditionalDataPending: aws.Bool(true),
		Marker:                aws.String("0"),
	}
	params := &rds.DownloadDBLogFilePortionInput{
		DBInstanceIdentifier: aws.String(c.Options.InstanceIdentifier),
		LogFileName:          aws.String(logFile.LogFileName),
	}
	for aws.BoolValue(resp.AdditionalDataPending) {
		// check for signal triggered exit
		select {
		case <-c.Abort:
			return logFile, fmt.Errorf("signal triggered exit")
		default:
		}
		params.Marker = resp.Marker // support pagination
		resp, err = c.RDS.DownloadDBLogFilePortion(params)
		if err != nil {
			return logFile, err
		}
		if _, err := io.WriteString(outfile, aws.StringValue(resp.LogFileData)); err != nil {
			return logFile, err
		}
	}
	return logFile, nil
}

// GetLogFiles returns a list of all log files based on the Options.LogFile pattern
func (c *CLI) GetLogFiles() ([]LogFile, error) {
	// get a list of all log files.
	// prune the list so that the log file option is the prefix for all remaining files
	// return the list of as-yet unread files
	logFiles, err := c.getListRDSLogFiles()
	if err != nil {
		return nil, err
	}

	var matchingLogFiles []LogFile
	for _, lf := range logFiles {
		if strings.HasPrefix(lf.LogFileName, c.Options.LogFile) {
			matchingLogFiles = append(matchingLogFiles, lf)
		}
	}
	// matchingLogFiles now contains a list of eligible log files,
	// eg slow.log, slow.log.1, slow.log.2, etc.

	if len(matchingLogFiles) == 0 {
		errParts := []string{"No log file with the given prefix found. Available log files:"}

		for _, lf := range logFiles {
			errParts = append(errParts, fmt.Sprint("\t", lf.String()))
		}
		return nil, fmt.Errorf(strings.Join(errParts, "\n"))

	}

	return matchingLogFiles, nil
}

func (c *CLI) GetLatestLogFile() (LogFile, error) {
	logFiles, err := c.GetLogFiles()
	if err != nil {
		return LogFile{}, err
	}

	if len(logFiles) == 0 {
		return LogFile{}, errors.New("No log files found")
	}

	sort.SliceStable(logFiles, func(i, j int) bool { return logFiles[i].LastWritten < logFiles[j].LastWritten })
	return logFiles[len(logFiles)-1], nil
}

// Gets a list of all available RDS log files for an instance.
func (c *CLI) getListRDSLogFiles() ([]LogFile, error) {
	var output *rds.DescribeDBLogFilesOutput
	var err error
	var logFiles []LogFile

	for {
		if output == nil {
			output, err = c.RDS.DescribeDBLogFiles(&rds.DescribeDBLogFilesInput{
				DBInstanceIdentifier: &c.Options.InstanceIdentifier,
			})
			logFiles = make([]LogFile, 0, len(output.DescribeDBLogFiles))
		} else {
			output, err = c.RDS.DescribeDBLogFiles(&rds.DescribeDBLogFilesInput{
				DBInstanceIdentifier: &c.Options.InstanceIdentifier,
				Marker:               output.Marker,
			})
		}
		if err != nil {
			return nil, err
		}

		// assign go timestamp from msec epoch time, rebuild as a list
		for _, lf := range output.DescribeDBLogFiles {
			logFiles = append(logFiles, LogFile{
				LastWritten:     *lf.LastWritten,
				LastWrittenTime: time.Unix(*lf.LastWritten/1000, 0),
				LogFileName:     *lf.LogFileName,
				Size:            *lf.Size,
			})
		}
		if output.Marker == nil {
			break
		}
	}

	return logFiles, nil
}

// ValidateRDSInstance validates that you have a valid RDS instance to talk to.
// If an instance isn't specified and your credentials contain more than one RDS
// instance, asks you to specify which instance you'd like to use.
func (c *CLI) ValidateRDSInstance() error {
	rdsInstances, err := c.getListRDSInstances()
	if err != nil {
		return err
	}

	if len(rdsInstances) == 0 {
		// we didn't get any instances back from RDS. not sure what to do next...
		return fmt.Errorf("The list of instances we got back from RDS is empty. Check the region and authentication?")
	}

	if c.Options.InstanceIdentifier != "" {
		for _, instance := range rdsInstances {
			if c.Options.InstanceIdentifier == instance {
				// the user asked for an instance and we found it in the list. \o/
				return nil
			}
		}
		// the user asked for an instance but we didn't find it.
		return fmt.Errorf("Instance identifier %s not found in list of instances:\n\t%s",
			c.Options.InstanceIdentifier,
			strings.Join(rdsInstances, "\n\t"))
	}

	// user didn't ask for an instance.
	// complain with a list of avaialable instances and exit.
	errStr := fmt.Sprintf(`No instance identifier specified. Available RDS instances:
	%s
Please specify an instance identifier using the --identifier flag
`, strings.Join(rdsInstances, "\n\t"))
	return fmt.Errorf(errStr)
}

// gets a list of all avaialable RDS instances
func (c *CLI) getListRDSInstances() ([]string, error) {
	out, err := c.RDS.DescribeDBInstances(nil)
	if err != nil {
		return nil, err
	}
	instances := make([]string, len(out.DBInstances))
	for i, instance := range out.DBInstances {
		instances[i] = *instance.DBInstanceIdentifier
	}
	return instances, nil
}

func (c *CLI) waitFor(d time.Duration) {
	select {
	case <-c.Abort:
		return
	case <-time.After(d):
		return
	}
}

// Nower interface abstracts time for testing
type Nower interface {
	Now() time.Time
}
