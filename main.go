package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/rds"
	flag "github.com/jessevdk/go-flags"

	"github.com/honeycombio/honeyrds/cli"
)

func main() {
	options, err := parseFlags()
	if err != nil {
		log.Fatal(err)
	}
	options.TempDir = os.TempDir()

	c := &cli.CLI{
		Options: options,
		RDS: rds.New(session.New(), &aws.Config{
			Region: aws.String(options.Region),
		}),
	}

	// make sure we can talk to an RDS instance.
	if err := c.ValidateRDSInstance(); err != nil {
		if err == credentials.ErrNoValidProvidersFoundInChain {
			log.Fatal(awsCredsFailureMsg())
		}
		log.Fatal(err)
	}

	if options.Backfill {
		fmt.Println("Running in backfill mode - downloading old logs and consuming them")
		err = c.Backfill()
	} else {
		fmt.Println("Running in tail mode - streaming slow query logs from RDS")
		err = c.Stream()
	}
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("OK")
}

// parse all the flags, exit if anything's amiss
func parseFlags() (*cli.Options, error) {
	var options cli.Options
	flagParser := flag.NewParser(&options, flag.Default)

	// parse flags and check for extra command line args
	if extraArgs, err := flagParser.Parse(); err != nil || len(extraArgs) != 0 {
		fmt.Println("Failed to parse the command line.")
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("Unexpected extra arguments: %s\n", strings.Join(extraArgs, " "))
	}
	// read the config file if specified
	if options.ConfigFile != "" {
		ini := flag.NewIniParser(flagParser)
		ini.ParseAsDefaults = true
		if err := ini.ParseFile(options.ConfigFile); err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("config file %s doesn't exist", options.ConfigFile)
			}
			return nil, err
		}
	}
	return &options, nil
}

func awsCredsFailureMsg() string {
	// check for AWS binary
	_, err := exec.LookPath("aws")
	if err == nil {
		return `Unable to locate credentials. You can configure credentials by running "aws configure".`
	}
	return `Unable to locate AWS credentials. You have a few options:
- Create an IAM role for the host machine with the permissions to access RDS
- Use an AWS shared config file (~/.aws/config)
- Configure credentials on a development machine (via ~/.aws/credentials)
- Or set the AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY environment variables

You can read more at this security blog post:
http://blogs.aws.amazon.com/security/post/Tx3D6U6WSFGOK2H/A-New-and-Standardized-Way-to-Manage-Credentials-in-the-AWS-SDKs

Or read more about IAM roles and RDS at:
http://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/UsingWithRDS.IAM.AccessControl.IdentityBased.html`
}
