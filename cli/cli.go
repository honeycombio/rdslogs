package cli

import (
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/rds"
)

// Options contains all the CLI flags
type Options struct {
	Region             string `long:"region" description:"AWS region to use" default:"us-east-1"`
	InstanceIdentifier string `short:"i" long:"identifier" description:"RDS instance identifier"`
	LogFile            string `short:"f" long:"log_file" description:"RDS log file to retrieve" default:"slowquery/mysql-slowquery.log"`
	Download           bool   `short:"d" long:"download" description:"Download old logs instead of tailing the current log"`
	DownloadDir        string `long:"download_dir" description:"directory in to which log files are downloaded" default:"./"`

	Version    bool   `short:"v" long:"version" description:"Output the current version and exit"`
	ConfigFile string `long:"config" description:"config file" no-ini:"true"`
	Debug      bool   `long:"debug" description:"turn on debugging output"`
}

// Usage info for --help
var Usage = `rdslogs --identifier my-rds-instance

rdslogs streams a log file from Amazon RDS and prints it to STDOUT.

AWS credentials are required and can be provided via IAM roles, AWS shared
config (~/.aws/config), AWS shared credentials (~/.aws/credentials), or
the environment variables AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY.

Passing --download triggers Download Mode, in which rdslogs will download the
specified logs to the directory specified by --download_dir. Logs are specified
via the --log_file flag, which names an active log file as well as the past 24
hours of rotated logs. (For example, specifying --log_file=foo.log will download
foo.log as well as foo.log.0, foo.log.2, ... foo.log.23.)`

// CLI contains handles to the provided Options + aws.RDS struct
type CLI struct {
	Options *Options
	RDS     *rds.RDS

	// memoize the list of log files
	cachedLogFiles []LogFile
	// allow changing the time for tests
	fakeNower Nower
}

// Stream polls the RDS log endpoint forever to effectively tail the logs and
// spits them out to STDOUT
func (c *CLI) Stream() error {
	// make sure we have a valid log file from which to stream
	logFiles, err := c.getListRDSLogFiles()
	if err != nil {
		return err
	}
	if err = validateLogFileMatch(logFiles, c.Options.LogFile); err != nil {
		return err
	}

	// forever, download the most recent entries
	sPos := StreamPos{
		logFile: LogFile{LogFileName: c.Options.LogFile},
	}
	for {
		// get recent log entries
		resp, err := c.getRecentEntries(sPos)
		if err != nil {
			return err
		}
		if resp.LogFileData != nil {
			io.WriteString(os.Stdout, *resp.LogFileData)
		}
		// if that's all we've got for now, wait 5 seconds then try again
		if !*resp.AdditionalDataPending || *resp.Marker == "0" {
			time.Sleep(5 * time.Second)
		}
		oldMarker := resp.Marker
		sPos.marker = c.getNextMarker(sPos, resp)
		if c.Options.Debug {
			if oldMarker == nil {
				s := "nil"
				oldMarker = &s
			}
			io.WriteString(os.Stderr, fmt.Sprintf("%s got %s as next marker, using %s\n",
				time.Now().Format("Jan 02 15:04"), *oldMarker, *sPos.marker))
		}
	}
}

// getNextMarker takes in to account the current and next reported markers and
// decides whether to believe the resp.Marker or calculate its own next marker.
func (c *CLI) getNextMarker(sPos StreamPos, resp *rds.DownloadDBLogFilePortionOutput) *string {
	// when we get to the end of a log segment, the marker in resp is "0".
	// if it's not "0", we should trust it's correct and use it.
	if *resp.Marker != "0" {
		return resp.Marker
	}
	// ok, we've hit the end of a segment, but did we get any data? If we got
	// data, then it's not really the end of the segment and we should calculate a
	// new marker and use that.
	if resp.LogFileData != nil && len(*resp.LogFileData) != 0 {
		newMarkerStr, err := sPos.Add(len(*resp.LogFileData))
		if err != nil {
			fmt.Printf("failed to get next marker. Reverting to no marker. %s\n", err)
			return nil
		}
		return &newMarkerStr
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
		return resp.Marker
	}
	// let's try again from where we did the last time.
	return sPos.marker
}

// StreamPos represents a log file and marker combination
type StreamPos struct {
	logFile LogFile
	marker  *string
}

// Add returns a new marker string that is the current marker + dataLen offset
func (s *StreamPos) Add(dataLen int) (string, error) {
	splitMarker := strings.Split(*s.marker, ":")
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
	}
	// if we have a marker, download from there. otherwise get the most recent line
	if sPos.marker != nil {
		params.Marker = sPos.marker
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
	fmt.Fprintf(os.Stderr, "Downloading log files to %s\n", c.Options.DownloadDir)
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

// Equal returns true if the timestamps and sizes are equal, even if the names
// are not. As AWS rotates log files, it changes the names every hour.
func (l *LogFile) Equal(candidate LogFile) bool {
	return l.LastWritten == candidate.LastWritten &&
		l.Size == candidate.Size
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

	if err = validateLogFileMatch(logFiles, c.Options.LogFile); err != nil {
		return nil, err
	}

	var matchingLogFiles []LogFile
	for _, lf := range logFiles {
		if lf.LogFileName == c.Options.LogFile ||
			strings.HasPrefix(lf.LogFileName, c.Options.LogFile) {
			matchingLogFiles = append(matchingLogFiles, lf)
		}
	}
	// matchingLogFiles now contains a list of eligible log files,
	// eg slow.log, slow.log.1, slow.log.2, etc.

	return matchingLogFiles, nil
}

func validateLogFileMatch(logFiles []LogFile, toMatch string) error {
	for _, lf := range logFiles {
		if lf.LogFileName == toMatch {
			return nil
		}
	}
	errParts := []string{"No log file with the given name found. Available log files:"}

	// TODO sort log files by timestamp
	for _, lf := range logFiles {
		errParts = append(errParts, fmt.Sprint("\t", lf.String()))
	}
	errParts = append(errParts, "Please specify one of these log files with the --log_file flag")
	return fmt.Errorf(strings.Join(errParts, "\n"))
}

// gets a list of all avaialable RDS log files for an instance
func (c *CLI) getListRDSLogFiles() ([]LogFile, error) {
	if c.cachedLogFiles != nil {
		// don't hit AWS twice for the same info
		return c.cachedLogFiles, nil
	}

	output, err := c.RDS.DescribeDBLogFiles(&rds.DescribeDBLogFilesInput{
		DBInstanceIdentifier: &c.Options.InstanceIdentifier,
	})
	if err != nil {
		return nil, err
	}

	// assign go timestamp from msec epoch time, rebuild as a list
	logFiles := make([]LogFile, 0, len(output.DescribeDBLogFiles))
	for _, lf := range output.DescribeDBLogFiles {
		logFiles = append(logFiles, LogFile{
			LastWritten:     *lf.LastWritten,
			LastWrittenTime: time.Unix(*lf.LastWritten/1000, 0),
			LogFileName:     *lf.LogFileName,
			Size:            *lf.Size,
		})
	}

	c.cachedLogFiles = logFiles
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

// Nower interface abstracts time for testing
type Nower interface {
	Now() time.Time
}
